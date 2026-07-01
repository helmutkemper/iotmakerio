# Case

The **Case** device picks **one** group of blocks to run, based on the value of
its selector. It replaces a stack of nested *If / Else* devices: instead of
chaining many two-way decisions, you list every branch side by side and let the
incoming value choose.

## The selector

The selector is the value wired into the port on the **left edge** of the Case
container (the `?` terminal). Its value at run time decides which case runs.
Connect an integer (or a boolean) to it.

## Cases

Each case has three parts:

- a **label** — a name for you, shown in the design-time dropdown at the top of
  the container so you can choose which case to view and edit on the stage. The
  label has no effect at run time.
- a **match** — how the selector value is compared:
  - **is** — matches one exact value (e.g. `is 5`).
  - **is any of** — matches any value in a list (e.g. `5, 2, 3, 4`).
  - **between** — matches an inclusive range (e.g. `between 1 and 10`).
  - **greater / less / greater-or-equal / less-or-equal** — matches a threshold.
- a **value** — the operand(s) the match uses.

One case may be marked **default**: it runs when no other case matches. A Case
without a default simply does nothing when nothing matches.

## Order matters

Cases are checked **top to bottom**, and the **first** one that matches wins.
This only changes the outcome when ranges or thresholds overlap — for example,
`between 1 and 10` placed before `is 5` makes the `is 5` branch unreachable,
because the range already caught the value. Use the up/down arrows to reorder.

## Preview and checks

The **Preview** tab shows the exact source code this Case generates in your
project's language (Go or C99). It is produced by the real code generator, so
what you see is what you get.

The editor also flags problems that come from the generated code itself:

- **two cases claiming the same value** — when every case is an exact value the
  Case becomes a `switch`, and a repeated value is a duplicate label that will
  not compile. This is reported as an **error** and blocks generation.
- **an empty range** (for example `between 10 and 1`, where the low bound is
  above the high bound) — the branch can never match. Reported as a **warning**.
- **an unreachable case** — a case whose values are already fully covered by an
  earlier case. Reported as a **warning**.

## Adding blocks to a case

Pick the case you want in the top dropdown, then drop blocks inside the
container while that case is shown. Each case keeps its own group of blocks; the
dropdown only changes which group is visible for editing.
