// A nested module on purpose: it needs headers that only exist after
// `make deps` clones and builds transcribe.cpp, so it must stay outside the
// root module's `./...` (the same reason whisper.cpp's Go bindings carry
// their own go.mod).
module tcspike

go 1.25
