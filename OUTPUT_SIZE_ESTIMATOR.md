# Adaptive Output-Size Estimator

This is the canonical guide to Plush's adaptive output-size estimator. It
combines the fundamentals, implementation design, formal runtime state model,
and complete validation record in one place.

The estimator predicts useful `strings.Builder` capacity before rendering. It
does not predict content, cache rendered output, or limit response size. A poor
prediction can affect allocation efficiency only; Plush still renders the same
output and the builder grows normally when needed.

## How To Read This Document

The document is arranged from approachable concepts to implementation detail:

1. **Part I: Plain-English Guide** explains templates, output buffers, learning,
   layouts, yield, headroom, ratio prediction, instability, and refinement
   without requiring familiarity with the VM internals.
2. **Part II: Technical Design** describes the complete algorithm, ownership,
   limits, controls, diagnostics, and measurement procedures for developers.
3. **Part III: Runtime State Model** specifies exact gates, states, transitions,
   concurrency behavior, reset behavior, diagnostic mapping, and invariants.
4. **Part IV: Validation And Evidence** records controlled measurements,
   sequential replays, cumulative validation, limitations, and reproduction
   commands.

Readers evaluating the idea can stop after Part I. Implementers should continue
through Parts II and III. Reviewers validating the design and its measured
behavior should also read Part IV.

## Quick Reference

- Estimation is enabled by default.
- Estimator diagnostics are off by default.
- The first successful render learns; later renders can use the observation.
- Complete-template estimates move halfway upward and one eighth downward.
- Stable root and layout plans can add headroom capped at 10% or 64 KiB.
- Loops learn bytes per item and multiply by the current item count.
- Layouts include the exact current yield and estimate only surrounding
  overhead.
- Layout profiles use competing absolute and ratio predictors.
- Unstable finite layout ranges can refine to depth 3 with at most 16 children.
- Learned speculative size is capped at 4 MiB; output size is not capped.
- Failed renders and invalid layout observations do not train successful state.
- State is bounded, concurrency-safe, and owned by compiled structures.

## Part I: Plain-English Guide

This part starts with the rendering basics and builds the estimator one idea at
a time. It is intended for readers who want to understand why the feature
exists and how it behaves before reading equations and state transitions.

### What Plush Does

Plush turns a template and some data into text, usually HTML.

For example, a template might contain:

```plush
<h1><%= title %></h1>
```

When `title` is `Examples`, Plush produces:

```html
<h1>Examples</h1>
```

Real templates also contain conditions, loops, helpers, partial templates, and layouts. Plush combines all of their output into the final response.

### Interpreter And VM

Plush can run a template in two ways.

The interpreter reads and executes the parsed template directly. The VM first compiles the template into reusable instructions and then executes those instructions on each render.

The output-size estimator is a VM optimization. It helps the compiled VM prepare memory for the response before executing those instructions.

### What An Output Buffer Is

While Plush is rendering, it needs a temporary place in memory to collect the generated text. That place is called an output buffer.

The buffer has two relevant sizes:

- **Length** is how much rendered text it currently contains.
- **Capacity** is how much text can fit before the buffer needs more memory.

If the buffer has enough capacity, Plush can keep adding output. If it becomes full, Go must:

1. Allocate a larger area of memory.
2. Copy all text already rendered into that new area.
3. Release the old area later.
4. Continue rendering.

The final HTML remains correct, but allocating and copying large buffers costs CPU time and creates temporary memory work.

### Why Plush Does Not Know The Size In Advance

The final response size depends on runtime data. A page may contain 5 records on one request and 500 on another. A condition may show or hide a large section. A partial or layout may add more dynamic output.

Plush can count literal HTML at compile time, but it cannot know the exact size of every dynamic value before rendering it.

### The Basic Estimator Idea

The estimator uses previous successful renders to predict how much capacity a compiled template will need next time.

```text
predict capacity -> reserve memory -> render normally -> measure actual size -> learn
```

The first render establishes a measurement. Before a later render, the VM reserves approximately the learned amount. When the prediction is good, the response can be written into one buffer without repeatedly allocating larger ones.

The estimator does not predict template content. It predicts only the number of bytes of capacity that will probably be useful.

### What Kind Of Algorithm This Is

The estimator uses a **bounded online adaptive partitioning algorithm**. It is similar to an adaptive histogram combined with an online predictor. It is not an AI model, a regression model, or a cache of rendered pages.

"Online" means that each successful render updates a few counters immediately. "Adaptive partitioning" means that a broad layout-size range can divide into smaller ranges when its renders are too different to share one useful estimate. "Bounded" means that this division has strict depth and memory limits.

The complete process has four cooperating parts:

1. Put the exact current layout body size into one of twelve initial ranges.
2. Learn both a typical fixed layout overhead and an overhead-to-body-size ratio for that range.
3. Select the model with the better rolling percentage-error history, while retaining conservative headroom for recent underestimates.
4. Split an unstable range in half after enough evidence, then let each half learn independently.

The ordinary byte estimate deliberately responds at different speeds:

```text
larger output: move halfway toward the new value
smaller output: move one eighth toward the new value
```

This fast-up, slow-down rule reduces repeated underallocation without allowing one unusually small response to erase useful capacity. Layout ratio and predictor-error histories use a symmetric one-eighth update so model selection can follow changing behavior without switching on every request.

### Before And After

Imagine a template will produce about 190 KB of HTML, but its output buffer begins much smaller. Without a useful estimate:

1. The VM fills the initial buffer.
2. Go allocates a larger buffer.
3. Go copies the existing HTML into the new buffer.
4. Rendering continues until that buffer is also full.
5. The allocation and copy may happen again.

With a learned estimate, the VM asks the buffer for approximately 190 KB before rendering starts.

In the common stable case, the builder allocates once, the VM writes the response, and no intermediate copy is needed.

The estimator is deliberately adaptive. It moves upward quickly when output grows and downward slowly when output shrinks. One unusually small response therefore does not immediately discard useful capacity, while repeated changes still update the prediction.

### What Plush Learns

#### Complete Templates

Each compiled template learns its normal final output size. The compiler's known static HTML supplies the initial hint, and successful renders improve it.

#### Loops

Loops learn approximately how many bytes one item produces. Plush multiplies that value by the current number of items, so a list containing 100 records receives a larger plan than the same list containing 10.

#### Partials

Each compiled partial learns its own output size. Plush measures only the bytes added by that partial, so unrelated page content does not train it.

#### How An Estimated Inner Page Becomes An Exact Yield

The inner page can contain static HTML, dynamic values, loops, conditions, helpers, and partials. Before it renders, Plush still has to estimate how much capacity its first builder will need:

```text
compiler-known static bytes
+ learned complete-template bytes
+ loop bytes-per-item plans
+ partial plans
= estimated capacity for the inner builder
```

Plush then executes every instruction and writes the real output. If the plan is too small, the builder grows normally. If it is too large, some capacity is unused. Either way, the completed result is a real Go string with an exact byte length.

Conceptually, the handoff looks like this:

```go
body := render(innerTemplate)
data["yield"] = template.HTML(body)
final := render(layoutTemplate, data)
```

Fast-plan and cached VM bytecode make the inner template execute efficiently. They cache reusable instructions and plans, not the dynamic HTML response. Loop and partial estimates help prepare the inner builder; they do not substitute for executing those loops or partials.

Once the inner render returns, Plush can use:

```go
yieldSize := len(body)
```

That length is exact in bytes, which is also the unit used by `strings.Builder` capacity. A useful summary is:

> The inner body is estimated while it is being built. After it is complete and becomes `yield`, its size is exact.

There are therefore two separate allocation plans:

```text
INNER BUILDER
plan with static, loop, partial, and previous output estimates
-> execute everything
-> produce an exact completed body string

OUTER LAYOUT BUILDER
plan with exact body/yield bytes + estimated layout overhead
-> execute the layout
-> produce exact final output
```

#### Composed Layouts

When an inner page is placed inside a layout, Plush already knows the exact byte length of the completed body. It only needs to predict the not-yet-rendered header, footer, navigation, and sections.

For example:

```text
exact completed yield:      220 KiB
estimated layout overhead:  500 KiB
outer builder plan:         720 KiB
```

After the layout finishes at 735 KiB, Plush can calculate the real surrounding overhead:

```text
actual overhead = 735 KiB final - 220 KiB exact yield
actual overhead = 515 KiB
```

That 515 KiB observation trains the selected layout profile for later renders.

Some layouts add a mostly fixed number of bytes. Others grow in proportion to the page body. Plush learns both patterns:

- `absolute` predicts a fixed amount of surrounding output.
- `ratio` predicts surrounding output as a proportion of the current body.

Both predictions receive rolling accuracy scores. Plush starts with the safer absolute model and selects ratio only after enough evidence shows that ratio is at least 12.5% better. It can return to absolute when template behavior changes.

#### Small Underestimates

An estimate can be less than 1% low and still force one more large allocation. Plush therefore learns a small headroom reserve when accurate predictions repeatedly fall just short.

That reserve shrinks when it is unnecessary and is capped at the smaller of 10% of the plan or 64 KiB.

#### Why The Estimate Moves Down By One Eighth

Underestimation and overestimation have different costs:

```text
underestimate -> allocate another buffer and copy existing output
overestimate  -> leave some capacity unused for this render
```

Plush therefore moves the ordinary byte estimate halfway upward after growth but only one eighth downward after shrinkage:

```text
larger actual: next = estimate + (actual - estimate) / 2
smaller actual: next = estimate - (estimate - actual) / 8
```

One eighth is exponential smoothing with a 12.5% update. The uncorrected difference is multiplied by `7/8` after every repeated observation. Its half-life is about 5.2 observations, and roughly 10% of the original difference remains after about 17 observations.

For an estimate of 1,600 KiB followed by repeated 900 KiB output:

```text
start:     1,600 KiB
after 1:   1,512 KiB
after 4:   1,310 KiB
after 8:   1,141 KiB
after 16:    983 KiB
```

Moving downward by one half would react faster but would oscillate badly when one small render is followed by another large render. Moving by one sixteenth would retain excess capacity for too long. One eighth is a measured engineering compromise, not a universal mathematical constant.

The ratio model and predictor-error scores move symmetrically by one eighth because they should follow changing behavior without switching models after every unusual request. Protective headroom separately handles recent underestimates.

#### Unstable Layout Bands

Every composed layout uses a base yield-size band, whether its output is stable or unstable. The root begins with twelve bands from `0-4k` through `4m+`.

For example:

```text
exact yield = 220 KiB
selected base band = 192k-256k
```

That base band learns absolute and ratio estimates, error scores, headroom, sample count, and lifetime minimum and maximum overhead. A stable band remains a base band forever and allocates no child state.

"Unstable" does not directly mean that one prediction missed the 10% accuracy target. It means the profile has observed a very wide output range:

```text
maximum observed overhead >= 4 * minimum observed overhead
```

A stable range might look like:

```text
390, 400, 420, 450 KiB
maximum/minimum = 1.15x
```

An unstable range might look like:

```text
180, 190, 750, 790 KiB
maximum/minimum = 4.39x
```

Instability and accuracy answer different questions:

```text
instability -> are the observed output shapes far apart?
accuracy    -> was this particular prediction close?
```

A cold stable profile can temporarily predict poorly. An unstable profile can still make some accurate predictions, especially when overhead follows yield size. Instability is a warning that trusting one shared estimate creates allocation risk.

As soon as a profile becomes unstable, speculative allocation uses its conservative observed minimum. Large forms can still grow naturally. After at least 32 observations, Plush asks whether separating the profile by exact yield size will create more consistent groups.

The render that records sample 32 has already used the parent plan. The next matching prediction creates the split:

```text
render with sample 31 -> predict from parent -> observe sample 32
next render           -> create children -> select one child
```

The interval is divided at its midpoint:

```text
                  192k-256k
                  /        \
          192k-224k        224k-256k
```

A 220 KiB yield selects the lower child. A 242 KiB yield selects the upper child. The decision uses yield bytes, not routes, request parameters, or record identities.

Children do not inherit the mixed parent average. Plush retains bounded summary statistics rather than every historical observation, so it cannot replay history into the correct children. Copying the average would also copy the mixture that caused the split.

Each child starts learning immediately. For its first four observations:

```text
learning:   record the child's actual overhead
allocation: use the immediate parent's conservative minimum
```

In the measured upper child, the parent minimum was about 182 KiB and the exact yield was about 252 KiB:

| Child samples before render | Plan | Actual | Result |
| ---: | ---: | ---: | --- |
| 0 | 434 KiB | 990 KiB | Grew naturally |
| 1 | 441 KiB | 901 KiB | Grew naturally |
| 2 | 441 KiB | 901 KiB | Grew naturally |
| 3 | 441 KiB | 900 KiB | Grew naturally |
| 4 | 963 KiB | 880 KiB | No growth |

The first four plans are intentionally conservative. The fifth plan uses the child's own four observations, selected absolute/ratio model, and normal safety policy.

Ideally, one unstable parent becomes two stable children:

```text
unstable parent
    -> stable lower child
    -> stable upper child
```

If one child is still unstable after collecting enough of its own observations, only that difficult branch splits again:

```text
                  224k-256k
                  /        \
          224k-240k        240k-256k
```

After splitting, Plush again asks whether the smaller groups are stable. Refinement stops when a child is stable or a fixed boundary is reached:

```text
maximum depth:          3
maximum children/root: 16
minimum child width:    8 KiB
```

##### What A Root Owns

A root is one compiled top-level template. It owns the twelve original layout bands and all children later created below those bands:

```text
compiled root template
|-- 0-4k
|-- 4k-16k
|-- ...
|-- 192k-256k
`-- 4m+
```

The depth and child limits apply to that compiled root, not to a URL or individual request.

##### How Maximum Depth Works

Depth counts how many child decisions occur below the selected base band:

```text
192k-256k             depth 0: base band
`-- 224k-256k         depth 1: first split
    `-- 240k-256k     depth 2: second split
        `-- 248k-256k depth 3: third split
```

At depth three, that branch cannot split again even if it remains unstable. Profile selection therefore requires no more than three lower-or-upper decisions after the base band is selected.

##### How The Child Budget Works

Every split creates exactly two new child profiles:

```text
first split:  lower child + upper child = 2 children
second split: two grandchildren         = 2 more children
total retained child profiles           = 4
```

The child that split remains in the tree as the parent of its grandchildren, so it still counts. This is why the measured root retained four children after two splits.

The sixteen-child limit is shared across all twelve base bands belonging to the compiled root:

```text
base band A creates 4 children
base band B creates 2 children
base band C creates 6 children
base band D creates 4 children
root total = 16 children
```

Once the root reaches sixteen children, another unstable band cannot add more. Sixteen children require at most eight split objects, or 1,472 structural bytes on the measured 64-bit build, plus short names and allocator overhead.

##### Why Children Must Be At Least 8 KiB Wide

The `16k-32k` base band is 16 KiB wide, so it can create two valid children:

```text
16k-32k
|-- 16k-24k   width 8 KiB
`-- 24k-32k   width 8 KiB
```

Those 8 KiB children cannot split again because doing so would create 4 KiB ranges. The `4k-16k` band is only 12 KiB wide and cannot split at all because its two children would each be only 6 KiB wide.

The width rule prevents Plush from creating tiny ranges that add state without reliably identifying a different layout shape.

##### Why Stable Children Stop

A profile is the bounded set of statistics attached to one base or child yield range. For example, the `192k-224k` profile handles completed yields in that range and remembers:

```text
sample count
absolute overhead estimate
overhead/yield ratio estimate
absolute and ratio error scores
minimum and maximum observed overhead
protective headroom
```

With fewer than two samples, the profile is not marked unstable, but it also has very little evidence. As observations accumulate, it remains stable while:

```text
maximum overhead < 4 * minimum overhead
```

A stable profile does not need to produce one constant size. These observations are close together and clearly stable:

```text
390, 400, 420, 450 KiB -> 1.15x range
```

This wider group is also stable under the 4x policy:

```text
200, 350, 500, 700 KiB -> 3.50x range
```

The second group may still produce an inaccurate individual prediction, but its lifetime range has not crossed the threshold that justifies more child state. Stability and accuracy answer different questions:

```text
stable/unstable -> how wide is the lifetime observed range?
within 10%      -> was this particular prediction close?
```

A cold stable profile can predict poorly. A stable profile can also miss after a sudden change. It continues learning on every valid render; stable does not mean frozen.

For every stable layout render, Plush:

```text
selects the profile using exact yield size
calculates absolute and ratio candidates
selects the model with the better rolling score
applies bounded protective headroom
reserves exact yield + predicted overhead
renders and records actual overhead
updates estimates, scores, minimum, and maximum
```

Stable profiles can use their learned model and headroom directly. An unstable profile instead limits speculative allocation to its conservative observed minimum or creates children after enough evidence.

A stable profile can become unstable later. Suppose it begins with:

```text
minimum = 180 KiB
maximum = 425 KiB
range = 2.36x -> stable
```

It later observes 800 KiB:

```text
minimum = 180 KiB
maximum = 800 KiB
range = 4.44x -> unstable
```

The lifetime minimum can only decrease and the maximum can only increase. Once that statistics node becomes unstable, it stays unstable until its compiled bytecode is replaced. Splitting gives future renders fresh child statistics instead of trying to erase the parent's history.

This is how an unstable parent can produce stable children:

```text
unstable parent observations:
180, 190, 220, 425, 482, 600, 790 KiB -> 4.39x

lower child observations:
180, 190, 220, 425 KiB -> 2.36x -> stable

upper child observations:
482, 600, 790 KiB -> 1.64x -> stable
```

The parent remains unstable, but future yields select one of the more consistent children. Both children continue updating their estimates, but neither creates descendants while it remains stable.

Adaptive child bands are created only because a parent proved unstable. Once they exist, all future yields in those subranges use them, and the desired result is for those children to remain stable.

In plain language, a stable profile means:

> This yield range has not shown enough lifetime variation to justify another partition. Continue learning and use the current prediction normally.

##### What Happens At A Boundary

If a profile remains unstable but cannot split because of depth, width, or child-count limits, Plush does not fail and does not discard output. It uses the conservative observed-minimum allocation plan and lets the builder grow normally when actual output is larger.

The boundaries answer different questions:

```text
depth 3      -> how far can one branch continue?
16 children  -> how much adaptive state can one root retain?
8 KiB width  -> how narrow can one yield range become?
stable child -> is another split needed at all?
```

Splitting has an information limit. Consider:

```text
yield 4.3 KiB -> final output 5 KiB
yield 4.3 KiB -> final output 414 KiB
```

Both forms select the same child because they present the same yield size. Narrower yield buckets cannot separate them. Plush keeps the conservative minimum plan: the small form avoids waste and the large form grows normally.

The complete refinement loop is:

```text
observe broad base band
-> detect a 4x lifetime range
-> wait for 32 samples
-> split on the next prediction
-> warm each selected child for four observations
-> let children predict independently
-> split only a still-unstable child
-> stop at stability or fixed limits
```

### What Happens on a Cold Start

New or recompiled bytecode has no history. Its first render uses compiler metadata and establishes the first sample. Later renders benefit from what was learned.

Changing and recompiling a template creates fresh estimator state. Data learned from an old version is not carried into a different version.

### Why It Is Safe

The estimate is only a capacity hint. It is not an output limit.

If Plush predicts 100 KB and the template produces 500 KB, Go simply grows the builder normally. The response is not truncated, skipped, or changed.

The estimator also:

- stores sizes and counters, not rendered HTML or request data
- does not create state per request path, record, or request
- starts with twelve fixed layout-size bands per compiled root template
- lazily refines only unstable layout bands, with a maximum depth of three and sixteen child profiles per root
- caps speculative learned growth while allowing real output to grow normally
- reduces aggressive preallocation when observed output varies by four times or more
- uses atomic values so cached bytecode can be rendered concurrently

On a 64-bit system, the twelve base nodes occupy 864 bytes and the profile owner occupies 16 bytes. A root that never refines stops there. Each split adds two child profiles in a 184-byte refinement object; the hard limit of sixteen children permits at most eight split objects, or 1,472 additional structural bytes plus short retained range-name strings and allocator overhead. State can grow only to this per-root bound, never with routes, records, or an unlimited number of requests.

### Where It Helps Most

The estimator provides the largest benefit when:

- compiled templates are reused many times
- responses contain substantial HTML
- loops render many records
- output is stable or changes in a predictable relationship to item count or body size
- a slightly low prediction would otherwise copy a large buffer

The benefit is smaller on the first render, on tiny one-off templates, or when database, network, and helper work dominate the request.

### Accuracy And Allocation Are Different

Suppose Plush plans 1,200 KiB and the actual output is 900 KiB:

```text
prediction error: about 33%
natural growth:   no
unused capacity:  about 300 KiB
```

The estimate fails the 10% accuracy target, but it still avoids another allocation and copy. Conversely, a plan can be only slightly low and still trigger expensive growth.

Estimator analysis therefore tracks:

```text
prediction error -> how close was the learned estimate?
natural growth   -> did the builder allocate and copy again?
unused capacity  -> did the plan reserve too much?
```

A useful policy balances all three. Prediction accuracy alone does not describe memory behavior.

### Measured Results

When `strings.Builder` is measured by itself while writing a 190,305-byte document in 7,170 precomputed chunks, one correct capacity reservation changes the median result from:

| Builder plan | Time | Allocated bytes | Allocations |
| --- | ---: | ---: | ---: |
| Natural growth | 123,971 ns | 908,770 bytes | 25 |
| One correct `Grow` | 40,164 ns | 196,608 bytes | 1 |

That is **67.6% less time, 78.4% fewer allocated bytes, and 96.0% fewer allocations** inside the builder-only workload. It is also 3.09 times the write throughput. This isolated result shows what repeated growth costs; it does not include Plush, estimator learning, or diagnostics.

On a generic template producing exactly 190,305 bytes:

- enabling the estimator made the VM 22.2% faster
- allocated bytes fell by 67.7%

On a deliberately alternating small/large workload:

- the VM was still 9.6% faster
- allocated bytes fell by 28.6%

An anonymized replay of 3,602 warmed composed-layout renders found that predictions below 10% error increased from 79.59% with the absolute-only layout model to 95.44% with adaptive model selection.

A separate steady-state engine benchmark found the final compiled VM 5.76 times faster than the parsed interpreter on the same 190,305-byte output. That larger difference represents the complete VM, not the estimator alone.

In a later 25,676-render capture, valid warm root estimates were below 10% error 98.62% of the time. One variable layout root still needed natural builder growth after 78.54% of its explicit plans because its shared band had become unstable. A sequential replay reproduced that baseline closely and projected that bounded refinement would reduce requested hints below actual output from 80.47% to 23.06% for that root. Its learned accuracy remained around 75%, demonstrating the limit: refinement can recover much of the allocation benefit when yield ranges differ, but it cannot identify genuinely different layouts that present the same yield information.

A later continuously running validation reached 63,358 renders over 124.18 minutes. It recorded 62,952 valid warm observations, with 98.78% below 10% error, 0.50% median error, 5.11% P95 error, and a 4.08% natural-growth rate.

The unstable upper profile that activated refinement changed as follows:

| Profile | Warm samples | Natural growth | Discarded capacity/render | Known capacity/output |
| --- | ---: | ---: | ---: | ---: |
| Unstable parent | 5 | 100% | 417.6 KiB | 1.829x |
| `224k-240k` child | 10 | 20% | 208.8 KiB | 1.457x |
| `240k-256k` child | 16 | 0% | 0 KiB | 1.432x |

The children reduced known builder backing-capacity traffic by approximately 20-22%. The upper child eliminated observed natural growth, but an early 2.2 MiB response caused temporary over-allocation when later responses were mostly 800-1,000 KiB.

Its grow hint moved downward as designed:

```text
1,770 KiB -> 1,661 KiB -> 1,553 KiB -> ... -> 1,125 KiB -> 1,010 KiB
```

The latest measured plan was 1,010 KiB for 890 KiB of output, 13.6% high, with no natural growth. This shows both sides of the policy: refinement removed repeated growth, while one-eighth decay gradually released excess capacity.

#### Latest Growth Validation

A later 22-minute, 45-second capture contained 12,097 unique renders. Every
record had a unique request identifier, a valid consumed yield, and a complete
estimator observation. The capture included 125 cold observations and 11,487
mature renders whose selected profile already had at least ten observations.

The mature results were:

| Profile state | Raw prediction below 10% error | Required natural builder growth | Cumulative unused capacity/output |
| --- | ---: | ---: | ---: |
| Stable | 99.54% | 3.39% | 5.44% |
| Unstable | 76.37% | 13.19% | 14.62% |
| Combined | 99.17% | 3.54% | 5.52% |

Here, "natural growth" means the builder's final capacity was larger than its
capacity immediately after Plush's explicit `Grow`. In other words, Go had to
allocate a larger backing buffer while rendering. The combined 3.54% rate
means that 96.46% of mature renders fit without that additional builder
growth. The unused-capacity figure is cumulative buffer slack divided by
cumulative output bytes; it is not retained process memory.

Cold behavior was intentionally different. A profile with no observation had
to grow naturally 92.8% of the time because Plush had not yet seen a real
output from which to learn. The first render establishes that measurement;
later renders receive the learned plan.

##### What headroom changed

Among 11,305 mature stable renders, the raw learned estimate was below the
actual output 5,086 times. Bounded headroom raised 3,723 of those plans to at
least the actual byte size. Go's allocation-size rounding absorbed most of
the small shortfalls left over, so only 383 stable renders required another
builder growth.

Headroom was modest:

```text
median applied headroom:  2,265 bytes
mean applied headroom:    6,805 bytes
final unused capacity:    5.44% of stable output bytes
```

This is the intended tradeoff. The final grow hint can be slightly farther
from the exact output than the raw prediction because it includes protective
capacity. That small reserve reduces allocation and copying without reserving
an entire worst-case response.

##### Why a large raw error can still produce a good grow plan

An unstable profile does not blindly allocate its raw learned average. It
uses its conservative observed-minimum policy. One recorded render reported a
219% raw-model error, but its actual allocation plan was:

```text
actual output:    187,788 bytes
final grow hint:  187,442 bytes
difference:           346 bytes, or 0.18%
```

The raw model was poor for that observation, but the bounded policy produced
a useful grow hint. Across all 182 mature unstable renders, 93.41% of final
grow hints were within 10% of actual output even though only 76.37% of raw
predictions met that target.

Plush marked 179 of those 182 unstable plans as limited, confirming that the
observed-minimum boundary, rather than an unbounded learned average, controlled
their speculative allocation.

The policy still protects memory when unstable output has a genuinely large
mode. One response produced about 737 KiB after a conservative plan of about
187 KiB. That response grew naturally. Reserving 737 KiB for every small
response in the same unsplittable profile would waste much more memory, so the
occasional growth is deliberate rather than a correctness failure.

##### Loops and partials

The same capture recorded 822,548 loop observations and 99,718 partial calls:

| Scope | Calls | Explicit grow calls | Limited calls |
| --- | ---: | ---: | ---: |
| Loops | 822,548 | 3.16% | 5,729 |
| Partials | 99,718 | 4.70% | 491 |

A loop's percentage error can look large when a few bytes change in a tiny
loop. In practice, the top-level builder usually already had enough free
capacity, so only 3.16% of loop observations issued another explicit grow.
Partials behaved similarly. Their low grow-call rates and bounded limit counts
show that nested estimates remained opportunistic rather than multiplying
speculative allocations.

This capture confirms that the growth policy remains useful after warming:
stable profiles usually complete in the planned buffer, unstable profiles
remain memory-bounded, and the fallback behavior always renders the complete
output.

### Seeing What It Decided

Render diagnostics expose:

- the raw prediction and actual output
- percentage error and whether it was below 10%
- selected layout predictor
- absolute and ratio candidates with their scores
- applied headroom and final grow hint
- builder capacity allocated and left unused
- bounded loop and partial details

This makes it possible to tell whether the prediction was accurate and whether that prediction actually prevented another allocation.

These measurements are information about the estimator; they are not all required for learning. Plush therefore has three diagnostic levels:

- **Off:** keep learning and reserving memory, but do not build estimator diagnostics.
- **Summary:** keep the page result and combined loop/partial totals, but skip individual detail records.
- **Detailed:** retain individual bounded loop and partial records for investigation.

The important distinction is that turning diagnostics off does not turn the estimator off. Plush still remembers sizes and calls `Grow`; it simply stops preparing the report about those decisions.

### Turning It Off

The estimator is enabled by default and can be disabled process-wide for comparison or troubleshooting:

```go
previous := plush.SetOutputSizeEstimatorEnabled(false)
defer plush.SetOutputSizeEstimatorEnabled(previous)
```

Disabled mode records no adaptive samples and uses only static compiler planning.

To keep estimation while reducing informational work, select summary or off diagnostics instead:

```go
previous := plush.SetOutputSizeEstimatorDiagnosticsMode(
    plush.OutputSizeEstimatorDiagnosticsSummary,
)
defer plush.SetOutputSizeEstimatorDiagnosticsMode(previous)
```

### Bottom Line

The feature helps by replacing repeated buffer growth and copying with one informed capacity reservation. It learns from successful renders, adapts to changing shapes, remains bounded in memory, and cannot change output correctness.

## Part II: Technical Design

This part presents the implementation as an engineering design: algorithm
classification, prediction layers, memory ownership, limits, diagnostics, and
controlled measurements.

### Overview

The Plush VM includes an adaptive output-size estimator for reusable compiled templates. It predicts the capacity needed by `strings.Builder` before rendering, reducing repeated buffer growth, memory copying, and temporary allocation.

The estimator is a capacity planner, not an output cache. It never stores rendered HTML, request values, context data, or route identities. A prediction can affect allocation efficiency, but it cannot change or limit rendered output.

The implementation covers:

- whole-template output
- fast-loop output per item
- composed layouts with an exact current `yield`
- bounded refinement of unstable layout bands
- compiled partial output
- unstable-output safeguards
- bounded diagnostics and a process-wide off switch

### Algorithm Classification

The implementation is a bounded online adaptive partitioner with asymmetric size smoothing, competing absolute/ratio predictors, and conservative cold-start fallback. It is closest to an adaptive histogram whose selected bin owns small online estimators; it is not statistical regression, content prediction, or route-based caching.

Its components are:

1. **Fixed base histogram:** exact layout `yield` size selects one of twelve initial bands.
2. **Online absolute estimator:** observed output or layout overhead moves halfway upward after growth and one eighth downward after shrinkage. A single upward observation is capped at four times the current estimate.
3. **Online ratio estimator:** layout overhead divided by exact yield is stored in Q20 fixed point and moves one eighth toward each observation in either direction.
4. **Competitive model selection:** rolling absolute-percentage errors are maintained for both candidates. Ratio is selected only when `ratio_error * 8 < absolute_error * 7`, a strictly greater than 12.5% advantage.
5. **Adaptive binary refinement:** after at least 32 samples, a profile whose lifetime maximum is at least four times its minimum can split its yield interval at the midpoint. Children learn independently.
6. **Protective planning:** recent underestimate headroom, unstable minimum policies, and a four-observation parent fallback protect allocation while estimates change.

The adaptive structure is constrained to three refinement levels, sixteen child profiles per compiled root, and children at least 8 KiB wide. These limits make both prediction time and retained state bounded. The selected profile and predictor influence only a `strings.Builder.Grow` hint; output correctness never depends on the prediction.

In compact form:

```text
select yield band -> optionally refine -> predict absolute and ratio
    -> choose lower-error model -> apply safety policy -> grow builder
    -> render -> validate observation -> update selected profile
```

### Runtime Flow

For each eligible render, Plush follows the same basic cycle:

1. Read static output metadata produced by the compiler.
2. Read the estimate attached to the cached bytecode.
3. Apply variability limits and bounded underestimation headroom.
4. Call `strings.Builder.Grow` with the resulting hint when more capacity is useful.
5. Render normally.
6. Record actual output only after successful rendering.
7. Use the updated estimate on later renders of the same compiled template.

The first render of newly compiled bytecode establishes the first sample. The next render is the first one that can benefit from that learned value.

### Estimation Layers

#### Compile-Time Metadata

The compiler counts bytes already known from literal HTML, constant writes, and static fast-plan segments. This becomes `StaticSize`, the first-render baseline.

Each bytecode object owns concurrency-safe statistics for its complete output, layout overhead, partial use, and eligible loops. Recompiling changed source creates new bytecode and fresh statistics, preventing one template version from training another.

#### Whole-Template Learning

Before a top-level render, Plush grows an empty builder using the best static, fast-plan, or learned hint. After a successful render, it records the builder's actual length. Failed renders, unrelated nested renders, and punch-hole sub-renders do not train whole-template statistics.

The moving estimate is deliberately asymmetric:

- The first valid sample becomes the estimate.
- When actual output is larger, the estimate moves halfway upward so underestimates recover quickly.
- When actual output is smaller, the estimate moves one eighth downward so one small response does not immediately discard useful capacity.
- A single upward sample contributes at most four times the current estimate.
- Learned values have a 4 MiB ceiling. This limits speculative allocation, not response size.

#### Adaptive Underestimation Headroom

Prediction accuracy and allocation success are related but different. An estimate can be less than 1% low and still force `strings.Builder` to allocate a larger backing array and copy everything already written. Stable whole-template and layout estimates therefore maintain a separate one-sided reserve:

```text
grow hint = raw learned estimate + bounded headroom
```

- When actual output exceeds the raw plan, headroom rises immediately to the observed shortfall.
- When the raw plan is sufficient, existing headroom decays by one eighth.
- Applied headroom is capped at the smaller of 10% of the planned size or 64 KiB.
- A first sample does not create headroom.
- Unstable output receives no headroom and continues using the observed-minimum policy.
- Partials and loops do not add independent reserves, avoiding repeated padding of the same root output.

The raw estimate remains unchanged and continues to drive accuracy reporting. Diagnostics expose `learned`, `headroom`, and the final `hint` separately.

For example, one measured render predicted 212,907 bytes and produced 214,285 bytes. The 0.64% error passed the accuracy target, but Go's initial 212,992-byte capacity was still too small and grew again to 270,336 bytes. That observation produces 1,378 bytes of headroom; after the normal estimate update, the next plan becomes 213,596 learned bytes plus 1,378 headroom bytes, or 214,974 bytes. A regression test preserves this case.

#### Per-Loop Learning

Each eligible fast loop learns a separate `bytes per item` value. After a successful loop, Plush divides produced bytes by rendered item count and applies the same fast-up, slow-down update.

On a later render:

```text
loop grow hint = learned bytes per item * current item count
```

Using the current item count lets the hint respond immediately when collection size changes. Explicit loop growth is capped at 256 KiB. Nested loops learn independently, and request diagnostics retain at most eight iterable-name/source-line details.

#### Composed Layouts

The current `yield` size is already exact, so Plush does not estimate it. It learns only the surrounding layout overhead:

```text
grow hint = exact current yield bytes + selected layout overhead + bounded headroom
observed overhead = final layout bytes - current yield bytes
```

Each profile maintains two overhead models:

```text
absolute candidate = moving average of observed overhead bytes
ratio candidate = current yield bytes * moving overhead/yield ratio
```

The absolute model is best when a layout contributes a mostly fixed header, footer, and navigation. The ratio model is best when surrounding sections grow with page content. Both candidates are scored after every successful render using absolute percentage error against the complete output, including the exact yield. The scores use a symmetric one-eighth moving update so recent behavior matters without letting one request replace the history.

Absolute is the cold-start default. Ratio selection requires at least two samples and a rolling error score at least 12.5% lower than absolute:

```text
select ratio when ratio error * 8 < absolute error * 7
```

If that condition stops being true, selection returns to absolute. Both models continue learning regardless of which one is selected. The ratio moves one eighth toward each new observation in either direction, with a four-times upward outlier limit. This symmetric transition avoids a lasting upward bias; bounded headroom separately protects against recent underestimates.

The root bytecode starts with twelve profiles for yield sizes from `0-4k` through `4m+`. The upper ranges use `64k-128k`, `128k-192k`, `192k-256k`, `256k-384k`, `384k-512k`, and `512k-1m` profiles so substantially different body sizes do not train the same model. Render-pass identity and template filenames associate layout work with the correct compiled root. No state is keyed by route, record, request value, or tenant identifier. The exact yield is included in full; selected layout overhead plus headroom is capped at 4 MiB.

##### Bounded adaptive refinement

If one base or refined profile has at least 32 observations and its maximum overhead is at least four times its minimum, the next prediction attempts to split its yield range at the midpoint. A range can split only when both children are at least 8 KiB wide. Selection can descend at most three levels, and a compiled root can allocate at most sixteen child profiles across all base bands.

Each child starts with independent absolute, ratio, error, headroom, minimum, and maximum state. For its first four observations, it learns normally but allocation uses the immediate parent's conservative minimum, raised by compiler static or cold fallback bytes when necessary. This avoids a tiny cold allocation immediately after refinement. Once warm, a stable child uses its own selected model and headroom. An unstable child either refines again within the limits or uses its own observed-minimum policy.

Refinement is installed with an atomic compare-and-swap. A prediction retains the exact selected node for its later observation, so a concurrent split cannot move an in-flight sample into a different child. Stable profiles never allocate child state.

Yield refinement cannot separate output shapes with the same yield size. The bounded unstable-output policy remains the final fallback for those cases.

#### Partials

Each compiled partial owns its own estimate. Plush measures the parent builder immediately before and after successful inline rendering, so the sample contains only that partial's output.

A partial grows the parent only when remaining capacity is insufficient. Explicit partial growth stops once the parent already has at least 64 KiB of capacity, leaving root and loop estimators as the primary planners.

#### Variable-Output Safety

Whole templates, layouts, and partials retain lifetime minimum and maximum observations. Output becomes unstable when the maximum is at least four times the minimum.

When output is unstable:

- whole-template growth uses at most the larger of static bytes and the observed minimum
- an eligible layout profile refines on its next prediction; while a child warms, allocation uses the immediate parent's conservative minimum
- a layout profile that cannot refine uses at most the larger of static bytes and its own observed minimum
- partial growth is capped at the larger of 64 KiB and static bytes
- loop growth continues to use current item count rather than a historical page total

These limits constrain explicit preallocation only. The builder can always grow normally when actual output is larger.

### Memory Boundaries

Estimator state is attached to compiled bytecode and has fixed bounds:

| State | Bound |
| --- | ---: |
| Whole-template statistics | One set per compiled bytecode |
| Partial statistics | One set per compiled partial bytecode |
| Loop statistics | One set per compiled eligible loop |
| Base layout profiles | Twelve sets per compiled root bytecode; 864 bytes for the base-node array on 64-bit systems |
| Layout profile owner | 16 bytes on 64-bit systems |
| Adaptive layout children | At most sixteen child profiles, stored in at most eight 184-byte split objects (1,472 structural bytes), plus short range-name strings and allocator overhead |
| Refinement depth | At most three midpoint selections |
| Refinement threshold | At least 32 samples and a 4x observed overhead range |
| Refined child warm-up | Four observations using the immediate parent's conservative minimum for allocation |
| Loop diagnostic details | Eight per request |
| Partial diagnostic details | Eight per request |
| Learned-size ceiling | 4 MiB |
| Stable underestimation headroom | 10% of planned size, up to 64 KiB |
| Explicit loop grow | 256 KiB |
| Unstable partial grow | 64 KiB or static size, whichever is larger |

State does not grow with request paths, input identities, or rendered values. Layout refinement grows only to the fixed per-root child budget.

### Disabling the Estimator

The estimator is enabled by default. It can be disabled process-wide at startup or around a controlled test:

```go
previous := plush.SetOutputSizeEstimatorEnabled(false)
defer plush.SetOutputSizeEstimatorEnabled(previous)
```

Disabled mode records no adaptive samples and applies no learned template, layout, partial, or loop hints. It preserves the VM's static fast-plan hint, providing a direct pre-estimator baseline without changing rendering behavior.

Diagnostic collection can be reduced without disabling the estimator:

```go
previous := plush.SetOutputSizeEstimatorDiagnosticsMode(
    plush.OutputSizeEstimatorDiagnosticsSummary,
)
defer plush.SetOutputSizeEstimatorDiagnosticsMode(previous)
```

`Off` keeps learning and growth but records no estimator diagnostics. `Summary` keeps root and aggregate loop/partial values without per-item details. `Detailed` keeps all bounded details. `Off` is the default. This separation allows production applications to retain allocation improvements without paying for debugging detail on every request.

### Diagnostics

Diagnostics expose the values needed to evaluate prediction quality and allocation behavior:

- raw estimate used before rendering
- bounded headroom and final grow hint
- actual output bytes
- updated estimate and sample count
- minimum, maximum, instability, and limit state
- selected layout predictor before and after observation
- base and refined layout bands, refinement depth, allocated child count, and parent-fallback state
- absolute and ratio layout candidates with rolling error scores
- static and fallback hints
- requested grow hint
- builder capacity before grow, after grow, and after rendering
- capacity allocated by explicit grow and final unused capacity
- whether a composed layout consumed its yield and therefore produced a valid accuracy sample
- loop totals and bounded per-loop bytes-per-item details
- partial totals and bounded per-file details

Percentage error is calculated from the pre-render estimate:

```text
error = abs(learned bytes - actual bytes) / actual bytes * 100
```

`within-10` is true only when error is strictly below 10%.

For composed layouts, accuracy is valid only when the final output is at least as large as the exact input yield. A conditional layout can legitimately choose not to emit an already-rendered yield; that produces `yield-consumed=0`, `accuracy-valid=0`, and no positive overhead observation. Accuracy reports must filter on `accuracy-valid` so the structurally inapplicable learned-versus-final comparison cannot dominate the mean.

### Isolated `strings.Builder` Benchmark

A dedicated benchmark measures only the buffer mechanism the estimator is intended to improve. It precomputes 7,170 string chunks that form the same 190,305-byte document used by the VM benchmark. Chunk generation, integer conversion, template execution, estimator learning, and diagnostics all happen outside the timed loop.

Both cases create a fresh `strings.Builder` and perform the same `WriteString` calls. The baseline lets the builder grow naturally. The planned case calls `Grow(190305)` once before writing. Results are medians of five 500 ms runs with `GOMAXPROCS=1` on an Intel i7-12700F:

| Builder plan | ns/op | B/op | allocs/op | CPU/time improvement | Byte-allocation improvement | Allocation-count improvement |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Natural growth | 123,971 | 908,770 | 25 | Baseline | Baseline | Baseline |
| One exact `Grow` | 40,164 | 196,608 | 1 | 67.6% less time | 78.4% fewer bytes | 96.0% fewer allocations |

One correct capacity plan saves 83,807 ns, 712,162 allocated bytes (695.471 KiB / 0.712162 MB), and 24 allocations per document. It processes the writes 3.09 times as fast. Go rounds the requested 190,305-byte capacity to a 196,608-byte allocator size, which accounts for the planned case's single allocation.

This benchmark is the isolated benefit available from avoiding natural builder growth. It does not include the cost of rendering, predicting, recording diagnostics, or other VM work. The end-to-end estimator benchmark below therefore reports a smaller CPU/time improvement even though it benefits from the same allocation reduction.

Reproduce the isolated measurement with:

```bash
make benchmark-strings-builder
```

### End-to-End Estimator Benchmark

#### Template Structure

The generic benchmark isolates output allocation from application work. It performs no network, database, helper, partial, or layout work. Plush compiles one template containing static HTML, three dynamic fields, and one fast loop:

```plush
<main><%= for (_, entry) in entries { %><article data-id="<%= entry.ID %>"><h2><%= entry.Name %></h2><p><%= entry.Content %></p></article><% } %></main>
```

Each record has an integer `ID`, a generated name such as `entry-42`, and an ASCII `Content` string of a fixed length. The exact output includes the varying number of digits in both `ID` and `Name`.

| Fixture | Records | Content per record | Exact rendered output | Decimal MB | Binary MiB |
| --- | ---: | ---: | ---: | ---: | ---: |
| Stable/large | 1,024 | 128 bytes | 190,305 bytes (185.845 KiB) | 0.190305 MB | 0.181489 MiB |
| Alternating small | 64 | 32 bytes | 5,625 bytes (5.493 KiB) | 0.005625 MB | 0.005364 MiB |
| Alternating large | 1,024 | 128 bytes | 190,305 bytes (185.845 KiB) | 0.190305 MB | 0.181489 MiB |
| Alternating mean | One small plus one large | - | 97,965 bytes/op (95.669 KiB) | 0.097965 MB/op | 0.093427 MiB/op |

#### Measurement Protocol

1. Each enabled or disabled sub-benchmark compiles its own template once outside the timed loop.
2. Disabled mode retains static planning but performs no adaptive learning.
3. Enabled mode is warmed before timing. The stable case learns the large shape; the alternating case observes both shapes so instability handling is active.
4. After `b.ResetTimer`, the stable case repeatedly renders one 1,024-record context. The alternating case switches between 64-record and 1,024-record contexts.
5. Each case runs five times for 500 ms with `GOMAXPROCS=1` on an Intel i7-12700F.
6. The table reports median `ns/op`; `B/op` and `allocs/op` were stable across samples.

`B/op` is total allocation traffic for one operation, including transient builder backing arrays, the result string, VM work, and diagnostics. It is not final response size or retained heap.

Because this is a single-core, in-memory benchmark with no I/O, `ns/op` is used as the CPU/time proxy. It does not represent whole-server CPU utilization.

#### Results

| Workload | Estimator | ns/op | B/op | allocs/op | CPU/time improvement | Allocation improvement |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| Stable, 190,305-byte output | Disabled | 293,940 | 629,072 | 781 | Baseline | Baseline |
| Stable, 190,305-byte output | Enabled | 228,827 | 203,152 | 782 | 22.2% faster | 67.7% fewer bytes |
| Alternating small/large output | Disabled | 156,786 | 317,696 | 395 | Baseline | Baseline |
| Alternating small/large output | Enabled | 141,692 | 226,739 | 399 | 9.6% faster | 28.6% fewer bytes |

For each stable render, estimation saves 65,113 ns and 425,920 allocated bytes (415.938 KiB / 0.406189 MiB). Allocation traffic falls from 3.306 times final output size to 1.068 times output size. Bounded loop observability adds one 16-byte allocation without offsetting the builder savings.

For each alternating operation, estimation saves 15,094 ns and 90,957 allocated bytes (88.825 KiB / 0.086743 MiB). This remains beneficial even though consecutive response sizes differ by approximately 34 times.

#### Profile Evidence

The stable-workload profiles confirm the allocation mechanism:

- With estimation disabled, natural `strings.Builder.WriteString` growth accounts for 83.48% of sampled allocation space.
- With estimation enabled, allocation moves to one planned builder grow.
- CPU samples in `runtime.memmove` fall from 620 ms to 350 ms.
- Cumulative builder-write time falls from 2.03 s to 1.11 s.

Profile totals cover different iteration counts because the faster configuration completes more operations during the profile window. Use `B/op` and `ns/op` for before/after percentages; use profiles to identify where CPU and allocation moved.

### VM Versus Interpreter

A separate steady-state benchmark renders the same stable/large fixture through a parsed interpreter template and a compiled VM template with estimation enabled. Parsing and compilation happen once outside the timed loops, the same context is reused, and the benchmark verifies that both engines produce the same 190,305 output bytes before timing. Results are medians of five 500 ms runs with `GOMAXPROCS=1` on the same Intel i7-12700F.

| Engine | ns/op | B/op | allocs/op | Relative result |
| --- | ---: | ---: | ---: | --- |
| Parsed interpreter | 1,291,215 | 1,412,701 | 15,142 | Baseline |
| Compiled VM with estimator | 224,193 | 203,152 | 782 | 5.76x faster, 85.6% fewer bytes, 94.8% fewer allocations |

This measures template-engine execution after each engine's reusable form has been prepared. It does not include parsing, VM compilation, network access, database work, or application helpers.

### Captured Layout Replay

A separate anonymized capture of 3,602 warmed, successful composed-layout renders was replayed in request order to evaluate the layout models against changing output shapes. This historical replay used compiled template filename plus the then-current eight fixed yield-size bands. It did not create route or input-specific profiles.

| Layout model | Root estimates below 10% error | Mean error | Selection share |
| --- | ---: | ---: | ---: |
| Absolute only | 79.59% | 6.97% | 100% absolute |
| Ratio only | 95.02% | 2.82% | 100% ratio |
| Adaptive dual selector | 95.44% | 2.74% | 88.24% ratio |

The dual selector improved the below-10% rate by 15.85 percentage points over the absolute-only model while retaining absolute overhead for shapes where it scored better. This is a deterministic accuracy replay, not a CPU or allocation benchmark; the controlled benchmark above supplies those measurements.

A later anonymized capture exercised 2,212 renders across 32 root template files. Replaying only plans with at least two prior samples reproduced the captured 96.7% below-10% rate, then evaluated the ratio transition and profile boundaries independently:

| Replay policy | Root estimates below 10% error | Mean error |
| --- | ---: | ---: |
| Eight bands, asymmetric ratio | 96.7% | 1.75% |
| Eight bands, symmetric ratio | 98.3% | 1.43% |
| Ten bands, asymmetric ratio | 97.2% | 1.56% |
| Ten bands, symmetric ratio | 98.7% | 1.29% |

These values are projections against recorded output shapes, not measurements from a deployment of the new policy. Fresh traffic and the controlled CPU/allocation benchmarks remain the final validation.

A fresh post-change capture then measured 5,827 renders across 49 root template files. After the first six seconds of a rolling restart, all profile names came from the ten-band symmetric policy. On the same high-volume root, warmed ratio predictions improved from 91.47% to 95.93% below 10% error; mean error fell from 3.73% to 2.68%, and P95 fell from 11.42% to 9.13%. This is direct fresh-traffic evidence that the combined symmetric-ratio and narrower-band policy helped that matched workload.

The capture also exposed two common output shapes that still shared `128k-256k`, plus a variable population in `256k-512k`. Replaying that capture with fixed midpoint boundaries produced:

| Replay policy | Root estimates below 10% error | Mean error | P95 error |
| --- | ---: | ---: | ---: |
| Ten bands | 93.4% | 3.95% | 11.79% |
| Add 192 KiB boundary | 97.9% | 1.39% | 5.38% |
| Add 192 and 384 KiB boundaries | 98.5% | 1.32% | 4.91% |

The twelve-band replay required eight additional cold plans in the 5,827-render capture and added 128 fixed bytes to the complete per-root layout profile. These are replay projections against recorded output shapes, not fresh deployed measurements of the twelve-band policy.

#### Fresh twelve-band capture and refinement replay

A later 43-minute capture measured 25,676 renders across 96 compiled root files. It contained 172 captured cold plans and 25,504 warm plans. One contextual render produced only 7 final bytes after receiving an 11,307-byte yield; because that layout did not consume its yield, it is structurally inapplicable to layout-overhead accuracy and motivated the explicit `yield-consumed` / `accuracy-valid` diagnostics.

Across the remaining valid warm samples, the deployed twelve-band policy measured:

| Scope | Warm samples | Below 10% error | Mean error | Median error | P95 error |
| --- | ---: | ---: | ---: | ---: | ---: |
| All roots | 25,503 | 98.62% | 1.37% | 0.41% | 5.39% |
| Non-error roots | 18,226 | 98.09% | 1.87% | 0.97% | 6.32% |
| High-volume root | 8,836 | 97.43% | 2.50% | 1.72% | 7.85% |
| Variable root | 466 | 75.54% | 9.24% | 3.06% | 38.64% |

The variable root put final outputs from approximately 395 KiB through 1.74 MiB behind yields concentrated between approximately 194 KiB and 256 KiB. Its unstable plans required builder growth after the explicit plan 99.12% of the time. This confirms that the observed-minimum policy protects memory but cannot provide the one-allocation fast path for a mixed profile.

The capture was then replayed sequentially through the bounded refinement implementation. The simulator reproduced the captured twelve-band headline at 98.62% below 10%, 1.37% mean error, and 5.39% P95 before refinement, providing a baseline check on the replay. Refinement produced one split pair on one root, 316 refined plans, eight parent-fallback warm-up plans, and two additional cold profile plans:

| Replay scope | Policy | Below 10% error | Mean error | P95 error | Requested hint below actual |
| --- | --- | ---: | ---: | ---: | ---: |
| All roots | Twelve base bands | 98.62% | 1.37% | 5.39% | 12.17% |
| All roots | Bounded refinement | 98.62% | 1.38% | 5.39% | 11.12% |
| Variable root | Twelve base bands | 75.54% | 9.10% | 38.64% | 80.47% |
| Variable root | Bounded refinement | 75.43% | 9.71% | 36.00% | 23.06% |

`Requested hint below actual` predicts whether natural growth is still needed; it is not a CPU benchmark and does not account for Go allocator rounding. For the unsplit variable-profile baseline, its 80.47% projection was close to the captured 78.54% post-plan builder-growth rate. The replay therefore projects a material allocation-path improvement for the unstable root while correctly showing that yield-only refinement cannot make its genuinely overlapping output shapes fully predictable.

An experimental trigger that also split stable profiles solely because both rolling model scores exceeded 10% was rejected. It created six children instead of two, added four more cold plans, increased variable-profile mean error from 9.71% to 10.79%, increased P95 from 36.00% to 53.70%, and did not improve the projected growth rate. The implemented policy consequently refines only profiles that meet the established 4x instability condition.

### Anonymized Multi-Template Validation

In addition to the controlled benchmark, an integration matrix exercised three unrelated real-world template sets across seven page shapes. Each combination received three consecutive requests, and the final warmed response was evaluated. A result passed when absolute learned-versus-actual error was strictly below 10%.

| Template set | Successful measurements | Root estimates below 10% | Partial aggregates below 10% | Partial aggregate outliers |
| --- | ---: | ---: | ---: | --- |
| Template A | 7/7 | 7/7 | 6/7 | 70.48% |
| Template B | 5/7 | 5/5 | 4/5 | 2129.44% |
| Template C | 7/7 | 7/7 | 4/7 | 18.89%, 170.32%, 10.47% |
| **Total** | **19/21** | **19/19** | **14/19** | Every successful root estimate passed. |

Two Template B responses were excluded from successful response totals. One generated 22,541,700 bytes (21.497 MiB) with a 0.00% root error in application diagnostics but failed at the transport boundary. The other ended in an application error before successful diagnostic response completion. Neither was counted as an estimator failure.

This integration matrix validates accuracy under real composition and variable data. It does not produce the CPU or allocation percentages above because complete requests also include application and infrastructure work. Partial aggregate outliers remain visible because absolute per-partial errors are summed rather than allowed to cancel; the final root estimate still passed for every successful measured response.

A follow-up run after adding dual-model selection repeated the seven shapes across all three template sets. Thirteen combinations completed with estimator diagnostics; every warmed root estimate was below 10% error. The selector retained `absolute` for all thirteen because its rolling score was lower for those stable layout-overhead shapes. Eight combinations returned application or transport errors before successful Plush diagnostics and were excluded rather than treated as estimator results.

### Reproducing the Measurements

Run the benchmark matrix:

```bash
make benchmark-strings-builder
make benchmark-output-estimator
make benchmark-render-engines
```

Write CPU and memory profiles to `/tmp/plush-output-estimator-profiles`:

```bash
make profile-output-estimator

go tool pprof -top /tmp/plush-output-estimator-profiles/enabled.cpu.pprof
go tool pprof -top -sample_index=alloc_space \
  /tmp/plush-output-estimator-profiles/enabled.mem.pprof
```

Run correctness and concurrency verification:

```bash
go test ./...
go test -race ./...
```

### Result

The estimator reduces builder allocation traffic and copy work for stable output while remaining beneficial under deliberately variable output. Its state is bounded, concurrency-safe, scoped to compiled bytecode, observable, and optional. Incorrect predictions can affect allocation efficiency only; they cannot affect rendered output correctness.

## Part III: Runtime State Model

This part is the normative state-and-transition reference. State names describe
conditions derived from atomic values on compiled structures; they are not
additional state objects stored by the VM.

### Purpose

This document specifies the runtime states and transitions of the Plush VM output-size estimator. It describes the implementation as it exists, including eligibility gates, integer update rules, layout predictor selection, instability, headroom, loops, partials, concurrency, and reset behavior.

The estimator does not use one stored state enum. Its effective state is derived from atomic counters and values on compiled bytecode. The state names below are therefore a precise model of conditions in the code, not additional runtime objects.

### Algorithm Family

Formally, the layout estimator is a bounded online adaptive binary partitioner over exact yield size. Each selected partition owns:

- an asymmetric online absolute estimator
- a symmetric online overhead/yield ratio estimator
- symmetric rolling percentage-error scores for both candidates
- lifetime range, headroom, and sample state

The partition tree begins as twelve fixed histogram bands. An unstable finite interval can split at its midpoint, producing two independent child estimators. Predictor selection and partition selection are recomputed on every plan; no trained model, selected-model enum, route key, or rendered data is retained.

This classification describes the combination of mechanisms. The exact transitions, thresholds, and bounds below are normative.

### State Owners

Estimator state belongs to compiled structures:

| Scope | Owner | Stored state |
| --- | --- | --- |
| Complete template | Compiled `Bytecode` | Estimate, headroom, samples, minimum, maximum |
| Compiled partial | Partial `Bytecode` | Estimate, samples, minimum, maximum; headroom exists but is not used by partial planning |
| Fast loop | Compiled `FastLoopPlan` | Bytes per item and samples |
| Composed layout | Root `Bytecode` | Twelve base profiles plus bounded lazy child profiles containing absolute estimate, ratio, two error scores, headroom, samples, minimum, maximum |

State is not keyed by route, request value, record identifier, or rendered output. Recompiling source creates new bytecode and fresh estimator state.

### Global Gate States

#### Disabled

Condition:

```text
OutputSizeEstimatorEnabled() == false
```

Effects:

- no learned template, layout, partial, or loop grow hint is applied
- no adaptive sample is recorded
- static compiler planning remains available to the VM
- estimator diagnostics report no adaptive output observation

The enable flag is checked before planning and again before observation. Disabling the estimator between those points prevents the render from training state.

Estimator diagnostic mode is independent from this lifecycle. `Off` suppresses root, loop, and partial diagnostic updates while the numeric estimator state continues to learn. `Summary` records root and aggregate loop/partial values but does not retain individual loop or partial detail slices. `Detailed` records both aggregates and bounded details. Diagnostic mode therefore changes observability cost, not prediction, growth, or successful-output learning.

#### Enabled But Ineligible

A complete-template or partial observation is ineligible when any required condition is absent. In particular:

```text
not top-level and not a named partial
or bytecode is nil
or the render is a punch-hole sub-render
```

A loop is ineligible when it is silent, has no statistics object, or estimation is disabled.

An ineligible render follows normal VM behavior without reading or updating adaptive state for that scope.

#### Enabled And Eligible

Eligible scopes enter the pre-render observation lifecycle described below. Eligibility does not guarantee a grow call; a hint may be zero or existing capacity may already be sufficient.

### Common Lifetime States

`OutputSizeStats` stores five atomic unsigned integers:

```text
estimate
headroom
samples
minimum
maximum
```

The following states are derived from them.

#### Cold

Condition:

```text
samples == 0
```

Properties:

- there is no learned estimate
- compiler static size and any fallback hint drive initial planning
- headroom is not applied
- a positive successful observation becomes the first sample

For complete templates, partials, and layout overhead, observations less than or equal to zero are ignored. Loop bytes-per-item observations accept zero but reject negative values.

#### Learning

Condition:

```text
samples == 1
```

Properties:

- the first positive observation is the current estimate
- minimum and maximum contain the first bounded observation
- the profile cannot yet be classified as unstable
- stable root/layout headroom may be applied on the next plan
- a layout still selects the absolute predictor

#### Stable

Condition:

```text
samples >= 2
and minimum > 0
and maximum / minimum < 4
```

Properties:

- the learned estimate participates in planning
- complete templates and layouts may apply learned headroom
- layout model selection may choose absolute or ratio
- normal fast-up, slow-down learning continues

#### Unstable

Condition:

```text
samples >= 2
and minimum > 0
and maximum / minimum >= 4
```

The division is integer division, matching the implementation.

Minimum can only decrease and maximum can only increase. Consequently, unstable is an absorbing state for the lifetime of that statistics object. It resets only when new bytecode is compiled.

Effects:

- complete-template and layout speculative growth is capped at the greater of compiler static size and lifetime minimum
- partial speculative growth is capped at the greater of 64 KiB and compiler static size
- root/layout headroom is not applied
- learning and range observation continue
- actual output remains unrestricted and the builder can grow normally

### Common Estimate Transition

Before updating, observations stored by `OutputSizeStats` are capped at 4 MiB. The cap limits learned preallocation state; it does not limit rendered output.

Let:

```text
E = current estimate
A = bounded actual observation
N = sample count before observation
```

#### First Sample

```text
if N == 0 or E == 0:
    next = A
```

#### Exact Match

```text
if A == E:
    next = E
```

#### Upward Update

First cap one observation to four times the current estimate:

```text
bounded_upward_actual = min(A, 4 * E)
step = max((bounded_upward_actual - E) / 2, 1)
next = E + step
```

This is the fast-up path.

#### Downward Update

```text
step = max((E - A) / 8, 1)
next = E - step
```

This is the slow-down path.

All divisions are integer divisions. The minimum one-byte step guarantees progress when values differ by less than the divisor.

### Observation Lifecycle States

Each eligible render moves through a short transaction-like lifecycle.

#### 1. Snapshot

Before rendering, the VM captures:

- current estimate and sample count
- lifetime minimum and maximum
- instability state
- selected layout candidate and scores, when contextual
- builder capacity before growth

This snapshot is used for both planning and request diagnostics.

#### 2. Plan

The VM applies scope-specific rules in this order:

```text
static/fallback floor
learned estimate or selected layout candidate
unstable-output cap
scope grow limit
stable root/layout headroom
```

#### 3. Optional Grow

The VM calls `strings.Builder.Grow` only when the hint is useful. Go may round the requested capacity to a different allocator size, so builder capacity after growth does not have to equal the hint.

The isolated builder benchmark quantifies this transition without VM work: for 7,170 writes producing 190,305 bytes, one correct grow reduced median time by 67.6%, allocated bytes by 78.4%, and allocation count from 25 to 1. These are builder-only measurements; complete estimator results include planning, rendering, and diagnostics and are reported in Part IV of this document.

#### 4. Render

The VM executes normally. Capacity is not an output limit. If the plan is too small, the builder grows naturally.

#### 5. Observe On Success

After successful rendering, the VM records actual produced bytes, updates state atomically, and writes post-render diagnostics.

#### 6. Skip On Failure

If rendering fails before the success observation, the estimate is not trained by incomplete output.

### Complete-Template Planning States

#### Cold Plan

For a direct top-level template with no samples:

```text
raw hint = max(fallback hint, compiler static size)
```

#### Learned Stable Plan

```text
raw hint = max(learned estimate, compiler static size)
final hint = raw hint + bounded headroom
final hint <= 4 MiB
```

#### Learned Unstable Plan

```text
raw hint = max(learned estimate, compiler static size)
final hint = min(raw hint, max(lifetime minimum, compiler static size))
```

No headroom is applied in the unstable state.

### Layout Profile States

#### Profile Selection

The root bytecode owns one lazily allocated array of twelve base yield bands:

| Band | Yield size |
| --- | ---: |
| `0-4k` | At most 4 KiB |
| `4k-16k` | More than 4 KiB, at most 16 KiB |
| `16k-32k` | More than 16 KiB, at most 32 KiB |
| `32k-64k` | More than 32 KiB, at most 64 KiB |
| `64k-128k` | More than 64 KiB, at most 128 KiB |
| `128k-192k` | More than 128 KiB, at most 192 KiB |
| `192k-256k` | More than 192 KiB, at most 256 KiB |
| `256k-384k` | More than 256 KiB, at most 384 KiB |
| `384k-512k` | More than 384 KiB, at most 512 KiB |
| `512k-1m` | More than 512 KiB, at most 1 MiB |
| `1m-4m` | More than 1 MiB, at most 4 MiB |
| `4m+` | More than 4 MiB |

The exact current yield is never estimated. A profile predicts only layout overhead:

```text
actual overhead = final layout size - exact yield size
```

A non-positive actual overhead is ignored by layout learning.

#### Adaptive Refinement State

Every base or child profile can own one immutable pair of child profiles. A prediction attempts to install that pair when all conditions hold:

```text
samples >= 32
and maximum / minimum >= 4
and refinement depth < 3
and current range width >= 16 KiB
and root child-profile count + 2 <= 16
```

The current range is divided at its integer midpoint. A yield equal to the midpoint selects the lower child; a larger yield selects the upper child. The 16 KiB parent-width rule guarantees both children are at least 8 KiB wide. The unbounded `4m+` base range cannot refine.

Refinement is checked during prediction, not observation. The observation that reaches the 32-sample threshold still updates its selected parent. A later prediction installs the children with compare-and-swap and selects one child.

Installing a split reserves two of the root's sixteen child-profile slots. A losing concurrent installer releases its reservation and uses the winning split. Therefore a root can retain at most eight split objects and sixteen child profiles across all base bands.

Each child starts with independent zeroed learning state. It records its own observations immediately. While:

```text
child samples < 4
```

the raw child predictor remains visible for learning diagnostics, but the allocation overhead uses:

```text
max(immediate parent minimum, compiler static size, cold fallback overhead)
```

No child headroom is applied or updated during this parent-fallback state. At four samples, the child uses its own normal predictor, instability, and headroom transitions.

An unstable child can refine again if all limits permit. At depth three, below the minimum width, or after the root exhausts sixteen children, the existing observed-minimum allocation policy remains active.

A prediction stores its selected profile node. Its later observation updates that same node even if another request concurrently installs a refinement. In-flight samples therefore do not move between profiles.

#### Layout Cold State

Condition:

```text
samples == 0
```

Selection:

```text
predictor = absolute
selected overhead = max(static size, fallback overhead)
```

The first positive overhead observation initializes both the absolute estimate and, when yield is positive, the overhead/yield ratio.

A cold refined child additionally enters the parent-fallback state above. The parent fallback controls allocation only; the child still initializes from its own observed overhead.

#### Layout Score-Collection State

Condition:

```text
samples == 1
```

Selection remains absolute. The second successful observation calculates the first comparable absolute and ratio error scores. The third plan is therefore the earliest plan that can select ratio.

#### Layout Absolute-Selected State

Condition:

```text
samples < 2
or ratio candidate == 0
or ratio_error_ppm * 8 >= absolute_error_ppm * 7
```

Selected overhead:

```text
selected overhead = absolute candidate
```

#### Layout Ratio-Selected State

Condition:

```text
samples >= 2
and ratio candidate > 0
and ratio_error_ppm * 8 < absolute_error_ppm * 7
```

Selected overhead:

```text
selected overhead = ratio candidate
```

Ratio must have a strictly greater than 12.5% score advantage. Equality does not select ratio.

Selection is recomputed from atomic values on every render. There is no stored selected-model flag. Both models continue learning in either selected state, allowing later renders to change the result.

#### Absolute Candidate Transition

The absolute candidate is the common output estimate applied to observed layout overhead. It uses the common 4 MiB cap and fast-up, slow-down transition.

#### Ratio Candidate Transition

The ratio is stored as fixed-point Q20:

```text
actual ratio = bounded overhead * 2^20 / yield
ratio candidate overhead = yield * learned ratio / 2^20
```

For a positive yield, ratio learning uses a symmetric transition:

- first ratio observation replaces zero
- upward changes move by one eighth, after a four-times outlier cap
- downward changes move by one eighth
- differing values move by at least one fixed-point unit

The resulting ratio candidate overhead is capped at 4 MiB. The common absolute estimate remains fast-up and slow-down; only the ratio is symmetric because bounded headroom separately protects its final grow plan from recent underestimates.

#### Predictor Error Transition

Each model is scored against complete output, not overhead alone:

```text
predicted total = yield + predicted overhead
actual total = yield + actual overhead
error ppm = abs(predicted total - actual total) * 1,000,000 / actual total
```

The stored error is capped at 1,000,000,000 ppm. The second observation initializes each available score. Later errors move the score symmetrically by one eighth, with a minimum one-unit step.

#### Layout Grow Plan

```text
raw overhead = max(selected overhead, compiler static size)
raw total = exact yield + min(raw overhead, 4 MiB)
final total = raw total + bounded headroom
final total <= exact yield + 4 MiB
```

On the cold plan, fallback overhead may also raise `raw overhead`. During refined-child warm-up, the immediate parent minimum controls allocation as specified above. In the unstable state, selected overhead is capped at the greater of lifetime minimum overhead and static size before the exact yield is added.

### Headroom States

Headroom is maintained separately from the raw estimate so accuracy reporting is not hidden by padding.

#### Headroom Inactive

Headroom is not tracked or applied when:

- there is no previous sample
- output was unstable at planning time
- a refined child is using its immediate-parent fallback
- the scope is a partial or loop
- estimation is disabled

#### Headroom Zero

Condition:

```text
stored headroom == 0
```

A sufficient prediction leaves it at zero. An underestimate can transition it to protective headroom.

#### Protective Headroom

When actual output exceeds the raw plan:

```text
shortfall = min(actual - raw plan, 64 KiB)
next headroom = max(current headroom, shortfall)
```

When the raw plan is sufficient:

```text
step = max(current headroom / 8, 1)
next headroom = current headroom - step
```

Applied headroom is:

```text
min(stored headroom, raw grow hint / 10, 64 KiB, remaining scope limit)
```

For a direct template, the comparison uses actual complete output and its raw template plan. For a layout, it uses actual overhead and the selected raw overhead plan, while the percentage application limit is calculated from the complete contextual grow hint.

If a render transitions the lifetime range into unstable, that render may still perform the headroom update selected from its pre-render stable snapshot. Subsequent unstable plans do not apply headroom.

### Partial States

#### Partial Cold

```text
samples == 0
hint = compiler static size
```

#### Partial Learned

```text
samples > 0
hint = max(learned partial size, compiler static size)
```

#### Partial Unstable

```text
hint = min(learned/static hint, max(64 KiB, compiler static size))
```

Partials do not apply independent headroom. In addition, an inline partial does not issue another explicit grow after the parent builder already has at least 64 KiB of capacity. The partial output is still rendered, and the parent builder can grow naturally.

### Loop States

Loop statistics contain:

```text
bytes_per_item
samples
```

#### Loop Cold

Condition:

```text
samples == 0
```

No adaptive loop grow is requested. A successful loop with at least one rendered item records:

```text
observed bytes per item = produced bytes / rendered item count
```

The division is integer division.

#### Loop Learned Without Known Count

When the iterable count is unavailable before execution, Plush records diagnostics and learns after rendering but cannot pre-grow from the current count.

#### Loop Learned With Known Count

Condition:

```text
samples > 0
and item count > 0
and learned bytes per item > 0
```

Plan:

```text
hint = min(learned bytes per item * current item count, 256 KiB)
```

Loop bytes per item uses the common fast-up, slow-down estimate transition. Loops do not track lifetime instability or headroom; using the current item count supplies their primary variability control.

### Concurrency Semantics

All learned scalar state uses Go atomic values. Updates use compare-and-swap loops.

Important consequences:

- readers do not block other renders
- two renders can plan from the same pre-render snapshot
- observations can complete in a different order from planning
- each successful compare-and-swap contributes a sample
- diagnostics describe the current render's snapshot and the state visible after its observation, which may include concurrent observations
- a layout prediction retains its selected node, so a concurrent refinement cannot redirect its observation
- refinement pairs are installed once with compare-and-swap and share the root's atomic child budget

The algorithm is concurrency-safe but intentionally not globally serialized. Estimates converge under repeated traffic without adding a per-template mutex to the render path.

The layout profile pointer is owned by compiled root bytecode, avoiding copies of active atomic state when other bytecode fields are shallow-copied for VM fallback execution.

### Reset Transitions

Estimator state resets when its owning compiled structure is replaced, including normal recompilation after source changes or cache invalidation.

```text
old compiled bytecode -> discarded with old estimator state
new compiled bytecode -> Cold
```

Disabling and re-enabling the estimator does not clear existing state. Disabled mode pauses adaptive planning and observation; re-enabled mode can read state previously learned by the same still-cached bytecode.

### Fixed Limits

| Limit | Value | Effect |
| --- | ---: | --- |
| Learned size/overhead | 4 MiB | Caps stored speculative estimate, not output |
| Single upward observation | 4x current estimate | Limits one-sample influence before half-step update |
| Applied headroom | 10% of raw hint | Percentage bound |
| Applied/stored shortfall headroom | 64 KiB | Absolute bound |
| Base layout profiles | 12 per compiled root | Covers all yield sizes without route-specific state |
| Adaptive layout children | 16 per compiled root | Permits at most eight lazy split pairs |
| Adaptive refinement depth | 3 | Bounds prediction traversal and specialization |
| Adaptive refinement minimum samples | 32 | Avoids splitting from a short transient |
| Adaptive refinement minimum leaf width | 8 KiB | Prevents arbitrarily narrow yield ranges |
| Refined child parent fallback | First 4 child observations | Avoids a tiny allocation while new child state warms |
| Layout overhead plan | 4 MiB plus exact yield | Exact yield is never capped by the estimator |
| Explicit loop grow | 256 KiB | Limits loop-level speculative grow |
| Unstable partial grow | At least 64 KiB or static size | Limits partial speculation |
| Inline partial parent threshold | 64 KiB capacity | Avoids repeated partial-level grows |
| Diagnostic loop details | 8 per request | Bounds observability state |
| Diagnostic partial details | 8 per request | Bounds observability state |

### Diagnostic State Mapping

Key output diagnostics map to state as follows:

| Diagnostic | Meaning |
| --- | --- |
| `available` | The render was eligible and had a statistics owner |
| `scope` | `template`, `file` for composed output, or `partial` internally |
| `profile` | Selected layout yield band |
| `refined-profile` | Selected base or child yield range |
| `profile-depth` | Number of midpoint refinements used for this selection |
| `profile-children` | Child-profile slots currently retained by the compiled root |
| `profile-fallback` | Allocation used the immediate parent's conservative minimum while a child warmed |
| `profile-fallback-min` | Immediate parent minimum supplied to that fallback |
| `yield-consumed` | Final output was at least as large as the exact input yield |
| `accuracy-valid` | The render is eligible for learned-versus-actual accuracy aggregation |
| `predictor` | Absolute or ratio selected from the pre-render snapshot |
| `predictor-after` | Selection visible after observation |
| `overhead-absolute` | Pre-render absolute candidate |
| `overhead-ratio` | Pre-render ratio candidate |
| `absolute-error-score` | Rolling complete-output error for absolute |
| `ratio-error-score` | Rolling complete-output error for ratio |
| `learned` | Raw pre-render estimate; it excludes applied headroom |
| `headroom` | Reserve actually applied to this plan |
| `hint` | Requested grow plan after policy and headroom |
| `actual` | Successfully rendered bytes |
| `estimate` | Learned estimate visible after observation |
| `samples` | Sample count visible after observation |
| `min` / `max` | Bounded lifetime observations |
| `unstable` | Lifetime maximum/minimum condition is at least 4x |
| `limited` | A variability or scope policy reduced/skipped speculative growth |
| `grow-allocated` | Capacity added by the explicit grow, after allocator rounding |
| `unused-cap` | Final builder capacity minus actual output length |

Accuracy is calculated from the raw pre-render estimate:

```text
abs(learned - actual) / actual * 100
```

`within-10` is true only when that value is strictly less than 10%.

For a contextual render, `accuracy-valid` also requires `actual >= yield`. If a layout condition does not emit its already-rendered yield, `yield-consumed=0`, `accuracy-valid=0`, and `within-10=0`. Its non-positive derived overhead is not learned. Aggregations must filter on `accuracy-valid` so such a render cannot distort percentage-error statistics.

### Correctness Invariants

The state model preserves these invariants:

1. A grow hint never truncates or limits output.
2. Adaptive state contains numeric measurements, not rendered content or request data.
3. Failed complete renders do not train successful-output state.
4. Changed compiled source receives fresh state.
5. Layout profile count is bounded to twelve base profiles plus sixteen child profiles and does not grow with routes or values.
6. Both layout predictors learn regardless of which one is selected.
7. Raw prediction remains separate from protective headroom.
8. Disabled mode records no new adaptive observations.
9. A contextual render that does not consume its yield cannot train layout overhead or count as a valid accuracy sample.

## Part IV: Validation And Evidence

This part records how the estimator was measured, why each policy was retained,
what cumulative validation demonstrated, and which limitations remain.

### Executive Summary

The Plush VM output-size estimator predicts useful `strings.Builder` capacity before rendering. It does not predict content and cannot limit or change output. A poor estimate can only make allocation less efficient; the builder still grows normally and rendering remains correct.

The implementation combines bounded online learning with adaptive yield-range partitioning:

- stable output learns a reusable byte estimate
- loops learn bytes per item and use the current item count
- partials learn independently per compiled partial
- composed layouts learn both absolute overhead and overhead relative to exact yield size
- unstable layout ranges can split into smaller independent ranges
- cold, unstable, and invalid observations have explicit safety policies

Controlled benchmarks measured:

- **22.2% less VM execution time and 67.7% fewer allocated bytes** for stable 190,305-byte output
- **9.6% less VM execution time and 28.6% fewer allocated bytes** for deliberately alternating output
- **67.6% less builder-only time, 78.4% fewer builder bytes, and 96.0% fewer builder allocations** when one correct reservation replaces natural growth

A cumulative 124.18-minute runtime capture then confirmed adaptive refinement in live execution. For the unstable upper range that triggered refinement, warmed children changed natural builder growth from 100% to 20% in one child and 0% in the other. The conservative known-capacity lower bound improved from 1.829 times output size to 1.457 and 1.432 times output size, approximately 20-22% less observed backing-capacity traffic.

The result is useful and bounded, but not magical. Yield-based partitioning cannot distinguish radically different output shapes that present the same yield size. An unusually large observation can also cause temporary over-allocation because the estimator deliberately moves downward slowly.

### The Allocation Problem

`strings.Builder` must allocate a backing buffer. If its capacity is too small, Go allocates a larger buffer and copies bytes already written. A large response can repeat that process several times.

The final output size is not generally knowable before execution because it depends on dynamic strings, conditions, collection lengths, partials, and layout behavior. Plush can count static bytes during compilation, but runtime information must be learned or measured.

The estimator follows this cycle:

```text
select state -> predict useful capacity -> apply safety policy
    -> grow builder -> render normally -> validate observation
    -> update the same selected state
```

The estimate is a capacity hint. It is never an output ceiling.

### Algorithm Family

The layout estimator is a **bounded online adaptive binary partitioner**. It is similar to an adaptive histogram in which every selected bin owns small online estimators. It is not AI, statistical regression, content caching, or route-based learning.

The full output-size system has five layers:

1. Compiler-known static byte counts provide a cold baseline.
2. Complete templates learn final output size.
3. Eligible loops learn bytes per item and multiply by the current item count.
4. Compiled partials learn only the bytes they append.
5. Composed layouts select an adaptive yield band and predict only the bytes surrounding the exact yield.

State belongs to compiled bytecode, loop plans, and compiled partials. It is not stored per route, request, record, or rendered value. Recompiling source creates new bytecode and fresh state.

### Online Absolute Estimate

Let:

```text
E = current estimate
A = current positive observation, capped at 4 MiB
```

The first observation initializes the estimate:

```text
if E == 0:
    next = A
```

Larger output moves the estimate upward quickly. One upward observation is first limited to four times the current estimate:

```text
bounded_actual = min(A, 4 * E)
next = E + max((bounded_actual - E) / 2, 1)
```

Smaller output moves the estimate downward slowly:

```text
next = E - max((E - A) / 8, 1)
```

This asymmetric fast-up, slow-down rule reduces repeated under-allocation while preventing one unusually small render from immediately discarding useful capacity.

### Absolute And Ratio Layout Models

For composed layouts, the current yield length is already exact. Each profile predicts only layout overhead:

```text
actual overhead = final output size - exact yield size
```

Every profile learns two candidates:

```text
absolute candidate = online estimate of overhead bytes
ratio candidate = exact yield * learned overhead/yield ratio
```

The ratio is stored in Q20 fixed point. It moves one eighth toward each new ratio in either direction, with the same four-times upward outlier limit. Both candidates receive rolling percentage-error scores against complete output:

```text
predicted total = yield + predicted overhead
actual total = yield + actual overhead
error ppm = abs(predicted total - actual total) * 1,000,000 / actual total
```

Error scores move one eighth toward each new error. Absolute is the cold default. Ratio is selected only with at least two samples and a strictly greater than 12.5% score advantage:

```text
select ratio when ratio_error * 8 < absolute_error * 7
```

Selection is recalculated on every render. Both candidates continue learning, so the selected model can change when template behavior changes.

### Protective Headroom

A prediction that is only slightly low can still force a large buffer copy. Stable root and layout profiles therefore retain recent underestimate headroom.

When actual output exceeds the estimate, the shortfall can raise headroom. When it is unnecessary, headroom decays by one eighth. Applied headroom is capped at the smaller of:

```text
10% of the current grow plan
64 KiB
```

Headroom protects against recent small misses. It is separate from the absolute and ratio models so those models do not need a permanent upward bias.

### Instability Policy

Each statistics object retains its lifetime minimum and maximum. It becomes unstable when:

```text
samples >= 2
and maximum / minimum >= 4
```

Minimum can only decrease and maximum can only increase, so instability remains set for that statistics object until bytecode is replaced.

An unstable profile limits speculative growth to a conservative observed minimum, raised by static or required fallback bytes. Large output is still rendered and the builder grows naturally. This policy protects memory when one average cannot represent multiple output modes.

### Bounded Adaptive Refinement

The root starts with twelve yield bands:

```text
0-4k, 4k-16k, 16k-32k, 32k-64k, 64k-128k,
128k-192k, 192k-256k, 256k-384k, 384k-512k,
512k-1m, 1m-4m, 4m+
```

A prediction attempts to split its selected profile at the interval midpoint when all conditions hold:

```text
samples >= 32
and maximum / minimum >= 4
and depth < 3
and interval width >= 16 KiB
and retained child count + 2 <= 16
```

The width rule guarantees children at least 8 KiB wide. The unbounded `4m+` range cannot split. A yield exactly equal to the midpoint selects the lower child.

Children begin with independent absolute, ratio, error, headroom, minimum, and maximum state. They learn immediately, but their first four allocation plans use the immediate parent's conservative minimum:

```text
child samples < 4 -> parent-minimum allocation fallback
child samples >= 4 -> child predictor and safety policy
```

An unstable child can split again within the same limits. At most three midpoint selections and sixteen child profiles can exist per compiled root.

Refinement is installed with atomic compare-and-swap. A prediction retains the exact node it selected, so its later observation updates that node even if another request concurrently creates a split.

### Invalid Layout Observations

A conditional layout can receive an already-rendered yield and legitimately choose not to emit it. If final output is smaller than the yield, `final - yield` is not a valid positive layout-overhead observation.

Such a render reports:

```text
yield-consumed=0
accuracy-valid=0
within-10=0
```

It does not train positive layout overhead and must be excluded from accuracy analysis. The final 124.18-minute capture rejected 160 such observations.

### Fixed Memory And Safety Bounds

On the measured 64-bit build:

| State | Bound |
| --- | ---: |
| Layout profile owner | 16 bytes |
| Twelve base layout profiles | 864 bytes |
| One split object containing two children | 184 bytes |
| Maximum split objects | 8 |
| Maximum adaptive structural state | 1,472 bytes plus short names and allocator overhead |
| Maximum refinement depth | 3 |
| Maximum child profiles | 16 |
| Learned grow hint | 4 MiB |
| Applied headroom | 10% of plan, at most 64 KiB |
| Explicit loop grow | 256 KiB |
| Unstable partial grow | 64 KiB or static size, whichever is larger |

State cannot grow with traffic volume, URLs, request identities, or record counts.

### Measurement Definitions

The runtime analysis uses the following definitions:

| Metric | Definition |
| --- | --- |
| Valid | `observed=true` and `accuracy-valid=true` |
| Cold | `samples-before=0` |
| Warm | Valid and `samples-before>0` |
| Warm child | Valid, `profile-fallback=false`, and `samples-before>=4` |
| Within 10% | `abs(raw learned - actual) / actual * 100 < 10` |
| Natural growth | `capacity-final > capacity-after-grow` |
| Discarded initial capacity | `capacity-after-grow` for a render that later grew naturally |
| Known capacity/output | `(final capacity + known discarded initial capacity) / actual output` |

Discarded capacity and known capacity/output are conservative lower bounds. A request log identifies the initial and final builder capacity but cannot reconstruct every intermediate allocation.

Prediction accuracy and allocation efficiency are related but different. A conservative overestimate can miss the 10% accuracy target while avoiding reallocation. It can still waste final unused capacity. Both dimensions must be reported.

Request duration is not a CPU benchmark because it includes application helpers, data access, network work, scheduling, and output sizes that differ between requests.

### Controlled Builder Benchmark

The isolated benchmark writes a 190,305-byte document in 7,170 precomputed chunks. It measures only builder allocation and writes.

| Builder plan | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Natural growth | 123,971 | 908,770 | 25 |
| One exact `Grow` | 40,164 | 196,608 | 1 |

One correct reservation measured:

- **67.6% less execution time**
- **78.4% fewer allocated bytes**
- **96.0% fewer allocations**
- 83,807 ns, 712,162 bytes, and 24 allocations saved per document

This is the isolated benefit available from avoiding natural growth. It excludes template execution and estimator overhead.

### End-To-End VM Benchmark

The generic benchmark renders one compiled template containing static markup, three dynamic fields, and a fast loop. Results are medians of five 500 ms runs with `GOMAXPROCS=1` on an Intel i7-12700F.

| Workload | Estimator | ns/op | B/op | allocs/op | CPU/time improvement | Allocation improvement |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| Stable, 190,305-byte output | Disabled | 293,940 | 629,072 | 781 | Baseline | Baseline |
| Stable, 190,305-byte output | Enabled | 228,827 | 203,152 | 782 | **22.2% faster** | **67.7% fewer bytes** |
| Alternating small/large output | Disabled | 156,786 | 317,696 | 395 | Baseline | Baseline |
| Alternating small/large output | Enabled | 141,692 | 226,739 | 399 | **9.6% faster** | **28.6% fewer bytes** |

Bounded observability accounts for the small enabled allocation-count increase. It does not offset the reduction in allocated bytes.

Profiles confirm the mechanism:

- natural `strings.Builder.WriteString` growth accounted for 83.48% of disabled allocation space
- enabled allocation moved to one planned builder grow
- `runtime.memmove` CPU samples fell from 620 ms to 350 ms
- cumulative builder-write time fell from 2.03 seconds to 1.11 seconds

Profile totals contain different operation counts because the faster benchmark completes more work in the same profile interval. `ns/op` and `B/op` are the before/after measures; profiles explain where the improvement occurred.

### VM Versus Interpreter Context

This separate benchmark measures the complete reusable template engines, not just estimation:

| Engine | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Parsed interpreter | 1,291,215 | 1,412,701 | 15,142 |
| Compiled VM with estimator | 224,193 | 203,152 | 782 |

The compiled VM was 5.76 times faster, allocated 85.6% fewer bytes, and performed 94.8% fewer allocations. Parsing and compilation were outside the timed loops.

### Predictor And Boundary Replays

Several sequential replays were used before live adaptive validation.

An anonymized 3,602-render layout replay produced:

| Model | Within 10% | Mean error |
| --- | ---: | ---: |
| Absolute only | 79.59% | 6.97% |
| Ratio only | 95.02% | 2.82% |
| Competitive absolute/ratio | **95.44%** | **2.74%** |

A separate 2,212-render replay showed why symmetric ratio learning and narrower bands were retained:

| Policy | Within 10% | Mean error |
| --- | ---: | ---: |
| Eight bands, asymmetric ratio | 96.7% | 1.75% |
| Eight bands, symmetric ratio | 98.3% | 1.43% |
| Ten bands, asymmetric ratio | 97.2% | 1.56% |
| Ten bands, symmetric ratio | **98.7%** | **1.29%** |

A 5,827-render replay then evaluated added fixed boundaries:

| Policy | Within 10% | Mean error | P95 |
| --- | ---: | ---: | ---: |
| Ten bands | 93.4% | 3.95% | 11.79% |
| Add 192 KiB boundary | 97.9% | 1.39% | 5.38% |
| Add 192 and 384 KiB boundaries | **98.5%** | **1.32%** | **4.91%** |

The resulting twelve-band policy added 128 fixed bytes per root compared with ten bands.

### Refinement Replay Before Deployment

A later 43-minute capture contained 25,676 renders across 96 compiled root files. It had 172 cold and 25,504 warm plans. Valid warm twelve-band results were:

| Scope | Warm samples | Within 10% | Mean | Median | P95 |
| --- | ---: | ---: | ---: | ---: | ---: |
| All roots | 25,503 | 98.62% | 1.37% | 0.41% | 5.39% |
| Non-error roots | 18,226 | 98.09% | 1.87% | 0.97% | 6.32% |
| High-volume root | 8,836 | 97.43% | 2.50% | 1.72% | 7.85% |
| Variable root | 466 | 75.54% | 9.24% | 3.06% | 38.64% |

The variable root required post-plan builder growth 78.54% of the time. Sequential replay reproduced the baseline and projected refinement:

| Scope | Policy | Within 10% | Mean | P95 | Hint below actual |
| --- | --- | ---: | ---: | ---: | ---: |
| All roots | Twelve bands | 98.62% | 1.37% | 5.39% | 12.17% |
| All roots | Bounded refinement | 98.62% | 1.38% | 5.39% | 11.12% |
| Variable root | Twelve bands | 75.54% | 9.10% | 38.64% | 80.47% |
| Variable root | Bounded refinement | 75.43% | 9.71% | 36.00% | **23.06%** |

Refinement projected a large allocation-path improvement without pretending that yield-only partitioning could make overlapping shapes fully predictable.

An experimental rule that split stable profiles whenever both predictor scores exceeded 10% was rejected. It created six children instead of two, added four cold plans, raised variable-root mean error from 9.71% to 10.79%, raised P95 from 36.00% to 53.70%, and did not improve projected growth. The deployed trigger therefore requires established 4x instability.

### Generic Multi-Template Integration

Three unrelated template sets were exercised across seven integration routes. Every successful warmed root estimate was below 10% error:

| Template set | Successful measurements | Root passes | Partial aggregate passes |
| --- | ---: | ---: | ---: |
| A | 7/7 | 7/7 | 6/7 |
| B | 5/7 | 5/5 | 4/5 |
| C | 7/7 | 7/7 | 4/7 |
| **Total** | **19/21** | **19/19** | **14/19** |

Two responses failed at application or transport boundaries before a successful diagnostic response and were excluded rather than counted as estimator failures. Partial aggregate outliers remained visible; they did not prevent every successfully measured final root from passing.

### Cumulative Live Validation

Four captures were taken from the same continuously running process. Each later archive contains the earlier period, so the rows are cumulative observations, not four independent trials. The comparison shows warm-up and state transitions over time; it is not a randomized A/B benchmark.

| Capture | Duration | Total renders | Valid warm | Within 10% | Median | P95 | Natural growth | Maximum depth |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| A | 15.90 min | 6,891 | 6,773 | 98.08% | 0.48% | 6.33% | 4.98% | 0 |
| B | 32.77 min | 16,054 | 15,902 | 98.76% | 0.49% | 5.13% | 4.32% | 0 |
| C | 85.40 min | 43,597 | 43,275 | 98.83% | 0.50% | 4.98% | 4.09% | 2 |
| D | 124.18 min | 63,358 | 62,952 | 98.78% | 0.50% | 5.11% | 4.08% | 2 |

Invalid observations correctly excluded from accuracy increased from 9 in Capture A to 160 cumulatively in Capture D. Global natural growth fell from 4.98% to 4.08%, an 18.1% relative reduction, while warm accuracy remained near 98.8%.

#### Same-Yield Limit

Capture A found one profile that alternated between approximately 5 KiB and 414 KiB of final output while presenting essentially the same 4.3 KiB yield. After the transition render, the unstable minimum policy planned the small 5,076-byte form exactly, leaving about 300 bytes unused. Large forms grew naturally.

This profile was in the `4k-16k` band, which is too narrow to produce two 8 KiB children. More importantly, both output forms had the same yield and would select the same child even if a smaller split were allowed. Lowering the split width would add state without solving the missing-information problem.

#### First Useful Split

In Capture B, one `192k-256k` profile became unstable on its 32nd observation. The final observation updates the parent; the next matching prediction installs the split. Historical samples were already separated by the midpoint:

| Future range | Samples | Yield bytes | Overhead bytes | Overhead max/min |
| --- | ---: | ---: | ---: | ---: |
| `192k-224k` | 7 | 206,085-220,310 | 182,033-425,403 | 2.34x |
| `224k-256k` | 26 | 233,905-261,376 | 482,394-793,390 | 1.64x |

Both prospective children were below the 4x instability threshold, showing that yield size contained useful separating information.

Capture C recorded the split, four upper-child fallback observations, and 29 warmed upper-child observations:

| Phase | Samples | Natural growth | Discarded capacity/render | Final unused/render | Known capacity/output |
| --- | ---: | ---: | ---: | ---: | ---: |
| Unstable upper parent | 5 | 100.00% | 417.6 KiB | 246.5 KiB | 1.829x |
| Upper child fallback | 4 | 100.00% | 430.0 KiB | 716.7 KiB | 2.272x |
| Warm `224k-256k` child | 29 | **27.59%** | **257.4 KiB** | **169.7 KiB** | **1.489x** |

Relative to the unstable upper parent, first-level warm refinement produced:

- **72.4% fewer natural growth events**
- **38.4% less discarded initial capacity per render**
- **31.2% less final unused capacity per render**
- approximately **18.6% less known backing-capacity traffic per output byte**

The first-level child later observed enough additional range to become unstable itself. On the prediction after sample 32 it split again:

```text
224k-256k
    -> 224k-240k
    -> 240k-256k
```

#### Warm Depth-Two Results

Capture D supplied the requested 10-20 warmed observations per depth-two child:

| Profile | Warm samples | Within 10% | Natural growth | Discarded capacity/render | Final unused/render | Known capacity/output |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `224k-240k` | 10 | 30.00% | **20.00%** | **208.8 KiB** | 223.6 KiB | **1.457x** |
| `240k-256k` | 16 | 6.25% | **0.00%** | **0 KiB** | 399.2 KiB | **1.432x** |

Compared with the unstable parent's 100% natural-growth rate and 1.829x known capacity/output:

- the lower depth-two child reduced growth by 80% and known capacity traffic by approximately 20.3%
- the upper depth-two child eliminated observed natural growth and reduced known capacity traffic by approximately 21.7%
- neither depth-two child was unstable at the end of the capture
- the root retained four children and stopped at depth two, below both configured limits

The upper child's low within-10 percentage does not mean allocation failed. Its fallback history contained a 2.2 MiB response, followed by mostly 800-1,000 KiB responses. Fast-up, slow-down learning intentionally retained excess capacity and avoided every natural growth event while converging downward:

| Child sample before render | Actual | Grow hint | Plan error | Natural growth |
| --- | ---: | ---: | ---: | --- |
| 4 | 1,665.4 KiB | 1,775.7 KiB | 6.6% | No |
| 5 | 859.2 KiB | 1,769.8 KiB | 106.0% | No |
| 10 | 832.6 KiB | 1,325.1 KiB | 59.2% | No |
| 15 | 874.4 KiB | 1,124.8 KiB | 28.6% | No |
| 19 | 889.8 KiB | 1,010.4 KiB | 13.6% | No |

The estimate was not stuck: its hint fell from 1,769.8 KiB to 1,010.4 KiB while preserving one-allocation rendering. This is the intended safety/performance tradeoff. If the smaller shape remains dominant, continued one-eighth decay will reduce unused capacity further.

### What The Live Data Proves

The cumulative live captures directly confirm that:

- the sample-32 instability trigger fires
- prediction after the threshold creates two midpoint children
- each child records independent observations
- the first four child plans use parent fallback
- warm children use their own estimates
- a still-unstable child can split at depth two
- root child accounting increases from two to four and remains bounded
- invalid yield measurements are excluded
- refinement materially reduces natural growth and temporary discarded capacity
- conservative downward learning can temporarily overallocate after a large observation

The captures do not isolate process CPU. Render durations include unrelated application work and changed output sizes. Controlled `ns/op`, `B/op`, and profiles remain the valid CPU and allocation benchmarks.

### Known Limitations

1. Cold renders cannot use history that does not yet exist.
2. Each new child intentionally spends four observations in conservative fallback.
3. Yield partitioning cannot separate different output modes with the same yield.
4. Fast-up, slow-down learning can retain excess capacity after a large outlier.
5. A refined child may need another bounded split when a broad range still mixes shapes.
6. Request latency cannot isolate estimator CPU from application and infrastructure work.
7. Accuracy below 10% and allocation efficiency must be evaluated separately.

These are bounded performance tradeoffs, not output-correctness risks.

### Disabling The Estimator

The estimator is enabled by default and can be disabled process-wide for a controlled baseline:

```go
previous := plush.SetOutputSizeEstimatorEnabled(false)
defer plush.SetOutputSizeEstimatorEnabled(previous)
```

Disabled mode retains compiler static planning but applies and records no adaptive template, layout, loop, or partial estimates.

### Reproducing Controlled Measurements

```bash
make benchmark-strings-builder
make benchmark-output-estimator
make benchmark-render-engines
make profile-output-estimator

go test ./...
go test -race ./...

go tool pprof -top /tmp/plush-output-estimator-profiles/enabled.cpu.pprof
go tool pprof -top -sample_index=alloc_space \
  /tmp/plush-output-estimator-profiles/enabled.mem.pprof
```

The implementation verification passed the normal and race test suites. `go vet ./...` reported only the pre-existing warnings in test helpers that copy a value containing `sync.Map`.

### Conclusion

The estimator has three independently supported results:

1. Controlled benchmarks prove that one good builder reservation materially reduces CPU time and allocation traffic.
2. Accuracy replays justify competitive absolute/ratio prediction, symmetric ratio learning, and the twelve base bands.
3. Cumulative live captures prove that bounded adaptive refinement activates, warms, recursively separates useful yield ranges, and reduces natural growth and known backing-capacity traffic on a genuinely unstable workload.

The strongest live result changed an unstable profile from 100% natural growth and 1.829x known capacity/output to 20% and 1.457x in one child, and 0% and 1.432x in the other. Global warm accuracy remained approximately 98.8% within 10% throughout refinement.

The implementation is therefore complete as a bounded capacity optimizer. Future tuning can improve convergence speed or policy tradeoffs, but it is not required for correctness, bounded memory, concurrency safety, observability, disablement, or demonstrated allocation benefit.
