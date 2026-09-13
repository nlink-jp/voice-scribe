// Package workdir resolves the directory one MCP call writes into: the
// caller's work directory, not the server's.
//
// The value is per session and per calling runtime, so the server cannot
// know it — only the caller can. Of the channels a runtime could use, the
// per-call argument is the only one all four of our callers have: MCP roots
// are empty from Codex and carry only the project directory from Claude
// Code, and Codex strips the environment before spawning a server, so a
// `${...}` expansion in a registration entry never arrives. The `_meta` key
// is the second channel, for the runtimes we write ourselves: they can set
// it on every tools/call without knowing any tool's schema.
//
// There is deliberately no fallback beyond those two. A server-chosen
// default is readable by the caller only by coincidence, and when it is not,
// the call still succeeds and returns a path to a file the caller cannot
// open — a failure with no symptom at the point it happens.
//
// Org ADR-021; project ADR-0010.
package workdir

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nlink-jp/voice-scribe/internal/mcp/mcpserver"
	"github.com/nlink-jp/voice-scribe/internal/mcp/toolerr"
)

// MetaKey is the request-level `_meta` key a runtime sets on every
// tools/call to name the session work directory.
const MetaKey = "jp.nlink/work_dir"

// Access mode bits for syscall.Access. Creating a workspace under the work
// directory needs both: write to add an entry, execute to traverse it.
const (
	wOK = 0x02
	xOK = 0x01
)

// deniedTrees are locations a work directory may never be, together with
// everything under them. They are the paths this server would be writing to
// on behalf of a model that named them, with the operator's own privileges
// and outside whatever sandbox the calling runtime applies to itself.
//
// Paths are in resolved form (/etc and /var are symlinks on darwin), because
// validation resolves symlinks before comparing.
var deniedTrees = []string{
	"/bin",
	"/sbin",
	"/usr",
	"/System",
	"/Library",
	"/Applications",
	"/private/etc",
}

// deniedExact are denied as the work directory itself but not as ancestors.
// /private/var holds the per-user temporary directory (/var/folders/...),
// which is a legitimate place for a caller to work in.
var deniedExact = []string{
	"/",
	"/private/var",
}

// Resolver resolves and validates work directories. The zero value is
// usable; Denied adds server-specific trees to the built-in list.
type Resolver struct {
	// Denied are extra absolute paths (and their subtrees) this server
	// refuses to treat as a work directory — its own data or config
	// directory, typically.
	Denied []string
}

// Resolve returns the validated work directory for one call: the tool's
// work_dir argument, else the runtime hint in the request's `_meta`, else
// an error. The returned path is absolute and symlink-resolved.
func (r Resolver) Resolve(ctx context.Context, arg string) (string, error) {
	dir := strings.TrimSpace(arg)
	if dir == "" {
		hint, err := metaHint(ctx)
		if err != nil {
			return "", err
		}
		dir = hint
	}
	if dir == "" {
		return "", toolerr.New(toolerr.CodeWorkDirRequired,
			"work_dir is required: pass the absolute path of a directory you can read back "+
				"(your session or working directory). Results come back as paths, and a path you cannot open is worth nothing.")
	}
	return r.Validate(dir)
}

// Validate applies the closed list of checks and returns the resolved path.
func (r Resolver) Validate(dir string) (string, error) {
	if strings.HasPrefix(dir, "~") {
		return "", toolerr.Newf(toolerr.CodeWorkDirInvalid,
			"work_dir %q starts with ~: nothing expands it on this path — pass the absolute path", dir)
	}
	if !filepath.IsAbs(dir) {
		return "", toolerr.Newf(toolerr.CodeWorkDirInvalid,
			"work_dir %q must be an absolute path", dir)
	}
	if hasParentSegment(dir) {
		return "", toolerr.Newf(toolerr.CodeWorkDirInvalid,
			"work_dir %q contains a .. segment; pass the path you mean", dir)
	}

	resolved, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		return "", toolerr.Newf(toolerr.CodeWorkDirNotFound,
			"work_dir %q does not exist — it is your directory, so this is a typo, not something to create here", dir)
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return "", toolerr.Newf(toolerr.CodeWorkDirNotFound, "work_dir %q: %v", dir, err)
	}
	if !fi.IsDir() {
		return "", toolerr.Newf(toolerr.CodeWorkDirNotFound, "work_dir %q is not a directory", dir)
	}

	if why := r.denied(resolved); why != "" {
		return "", toolerr.Newf(toolerr.CodeWorkDirDenied,
			"work_dir %q is refused: %s", dir, why)
	}
	if err := syscall.Access(resolved, wOK|xOK); err != nil {
		return "", toolerr.Newf(toolerr.CodeWorkDirNotWritable,
			"work_dir %q is not writable by this server", dir)
	}
	return resolved, nil
}

// denied reports why the resolved path may not be a work directory, or "".
func (r Resolver) denied(resolved string) string {
	home, err := os.UserHomeDir()
	if err == nil {
		if h, herr := filepath.EvalSymlinks(home); herr == nil {
			home = h
		}
		if resolved == home {
			return "it is the home directory itself; pass a directory inside it"
		}
	}
	for _, d := range deniedExact {
		if resolved == d {
			return "it is a system directory"
		}
	}
	for _, d := range deniedTrees {
		if within(resolved, d) {
			return "it is inside the system directory " + d
		}
	}
	for _, d := range r.Denied {
		if d == "" {
			continue
		}
		if e, err := filepath.EvalSymlinks(d); err == nil {
			d = e
		}
		if within(resolved, filepath.Clean(d)) {
			return "it is inside this server's own directory " + d
		}
	}
	return ""
}

// within reports whether path is root or lies under it.
func within(path, root string) bool {
	if path == root {
		return true
	}
	return strings.HasPrefix(path, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator))
}

// hasParentSegment reports whether the path has a ".." component.
func hasParentSegment(p string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// metaHint reads the work directory a runtime attached to the request. A
// present but non-string value is an error rather than a silent miss: a
// runtime that sets the key wrongly should hear about it once, not have
// every call fall through to "work_dir is required".
func metaHint(ctx context.Context) (string, error) {
	raw, ok := mcpserver.RequestMeta(ctx)[MetaKey]
	if !ok {
		return "", nil
	}
	var dir string
	if err := json.Unmarshal(raw, &dir); err != nil {
		return "", toolerr.Newf(toolerr.CodeWorkDirInvalid,
			"request _meta[%q] is not a string", MetaKey)
	}
	return strings.TrimSpace(dir), nil
}
