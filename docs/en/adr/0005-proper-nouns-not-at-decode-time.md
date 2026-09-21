# ADR-0005: Do not fix misrecognized proper nouns at decode time

- Status: Accepted
- Date: 2026-08-09

## Context

In v0.1.3, measurement established that `--prompt` (whisper's initial prompt) **cannot fix
the mishearing of a particular proper noun**. Against real audio in which a surname came
out wrong consistently, four kinds of prompt containing the correct spelling (kanji,
katakana, a list, inside a sentence) all failed, and some of them broke other lines that
were correct.

The candidate that remained was whisper.cpp's `whisper_full_params.grammar_rules`. A hint
was left in AGENTS.md: "if you really want to constrain the vocabulary, this is the
mechanism." **This ADR is the result of testing that hint.**

This comes right after writing that `--prompt` was "Cheap and effective" without measuring
it and being largely wrong, so this time the upstream implementation is read and real audio
measured **first**, and written up after.

## Mechanism (checked against the upstream source)

whisper.cpp `592feef0`, the submodule's pin, was read.

1. **A grammar constrains the shape of the whole segment, not the vocabulary.** `root` has
   to accept the **whole** output of the 30-second window. The bundled grammar examples
   (`colors.gbnf` / `assistant.gbnf` / `chess.gbnf`) are all closed command sets, used from
   the `command` and `wchess` examples. That is the designed use.

2. **The penalty is soft and only subtracts.** `whisper_suppress_invalid_grammar` is in
   substance a single line, `logits[reject.id] -= grammar_penalty` (default 100.0). **There
   is no processing that lifts a token the grammar accepts.** The only tokens touched are
   "tokens the grammar rejected".

3. **The penalty on EOT is disabled.** The block that "penalizes EOT when the grammar
   allows continuation" is still commented out upstream. That is, the decoder **can escape
   the grammar by cutting the segment short**.

4. **The grammar is initialized per 30-second window** (`whisper_grammar_init` on the
   decoder at each iteration). A constraint spanning windows cannot be written.

5. Passing `--grammar` **switches sampling to beam search** (cli.cpp). Compared naively,
   the effect of the grammar and the effect of the search strategy mix together.

A prediction follows from (2). **Adding the name as an alternative to a permissive grammar
does nothing, because nothing is rejected.** What follows is the test of that prediction.

## Measured

2026-08-09, M2 Max / macOS 26.6.1. Upstream `whisper-cli` built from the pin in a separate
tree and used from there (the repository's `build/` was not touched). 50 seconds of real
audio, Japanese, beam size 5 held fixed in every condition (to remove the confound in (5)).
The surname in question appears 3 times in those 50 seconds.

The surname is written **N** below (neither the correct spelling nor the wrong one has any
bearing on the content of the measurement).

| # | Grammar | Output |
|---|------|------|
| A | None | An ordinary transcript. **N comes out wrong all 3 times** |
| B | `piece ::= N \| [^\x00]` (N plus anything) | **Byte-for-byte identical to A** |
| C | A closed vocabulary of 7 words (including N) | **The 50 seconds collapse into 2 sentences**, and N never appears |
| D | C plus `--grammar-penalty 10` | Identical to C |
| E | `root ::= N [^\x00]*` (fixed at the start) | Emits **only the first character of N** and stops |

The identity of A and B **held with a different model too** (two of them: `ggml-base-q5_1`,
and the `kotoba-whisper-v2.2-ggml-q5_0` still on hand — byte-identical to the author's
v2.0, as ADR-0004 records).

What can be read from this:

- **B has no effect.** As predicted, a permissive grammar rejects nothing, so nothing
  happens. A "declare the vocabulary" use **does not exist**.
- **C bit. But it broke things.** The mechanism itself works — the output falls within the
  permitted vocabulary. But 50 seconds of conversation became 2 sentences, and **N, which
  occurs 3 times in the audio, never came out**. Even though it was in the vocabulary.
- **Cutting the penalty to 1/10 in D gives the same as C.** It is not a question of how
  hard the penalty subtracts.
- **E is a demonstration of mechanism (3).** Fix the start and it cuts off after one
  character. Ending the segment is cheaper than satisfying the grammar, so the decoder
  chooses that.

So grammar-constrained decoding is **not a mechanism for giving a vocabulary hint to an
open transcript.** It is a mechanism for forcing audio known to say nothing outside a
closed set into that set. It does not fit our use (transcribing a conversation where what
will be said is unknown) in principle.

## Decision

### 1. No grammar constraint goes into decoding

`--grammar` is not added. The hint left in v0.1.3 that "grammar_rules is the real
mechanism" **was wrong and is withdrawn** (AGENTS.md is corrected).

The route of enabling the upstream EOT penalty locally to get around (3) is not taken.
Changing, in our build alone, something upstream has deliberately disabled breaks on every
submodule update.

### 2. Fixing proper nouns is the **agent's** job, and voice-scribe does not carry it

This ADR initially proposed "keep a substitution table in voice-scribe and record it in
metadata". **That is withdrawn.** There are two reasons.

- **It can only fix the spellings that were enumerated.** Measured, one and the same
  surname split into **5** different errors (hiragana, katakana, one with a long-vowel mark,
  different kanji). **An agent that knows the cast can fix a sixth one nobody enumerated,
  from context.** A substitution table cannot do that.
- **The provenance concern does not hold.** The proposal rested on "if the agent rewrites
  it, what was heard and what was corrected become indistinguishable", but **the transcript
  file voice-scribe wrote stays untouched**. A diff can be taken at any time. There is no
  need for voice-scribe to perform the substitution on anyone's behalf.

voice-scribe's responsibility stays at "**returning a transcript that can be fixed**" —
accurate timestamps, segments that correspond to the actual speech, and not staying silent
when the result looks suspect (v0.1.3's over-splitting warning). Interpreting meaning can
only be done by the side that holds the context.

### 3. The conditions under which the grammar constraint comes back

It is an effective mechanism when building closed command recognition (voice commands,
confirming a fixed read-back). At that point it takes the form of passing a GBNF file
through as **a separate feature**. It is not mixed into the options of general-purpose
transcription — C and E are the reason.

## Consequences

- The README's statement that "a stubborn name should be fixed after transcription" was
  correct. Decision 2 is what settles the "who" of it — not voice-scribe, but the
  downstream that holds the context
- **Pre-processing does not fix it either.** Source separation was measured and the name did
  not change (ADR-0006). Proper nouns are not a problem on the acoustic side
- The mention of grammar in AGENTS.md **has been corrected** (replaced with a link to this ADR)
- Investigation in the direction of "constraining the vocabulary at decode time" stops
  here. So that the same road is not walked again, the conditions that had no effect (B and
  D) are left in the table above
- The procedure for building the upstream `whisper-cli` turned out to be useful for
  measurement. In future, the same move can be used to measure the effect of a parameter
  our Go bridge does not expose
