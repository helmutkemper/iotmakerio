# Chapter 1 — Creating your first C99 device

> This chapter is for people who have never used IoTMaker. By the end of
> it you will have created a device called `stdOut`, with three blocks —
> `print_int`, `print_float`, and `print_string` — and used one of them
> inside a project to generate C99 source code.
>
> Estimated time: 20 minutes. Prerequisite: basic C knowledge
> (being able to write a simple function).

---

## 1.1 What is a device?

In IoTMaker, a **device** is a graphical box that the user drags onto the
**stage** (the IDE's working area) and connects to other boxes using
wires. Each wire carries a value — a number, a text, a signal.

Behind every device there is real code. When you are a **specialist**,
you write that code once and IoTMaker turns it into graphical blocks.
From then on, anyone — including people who have never programmed — can
use your blocks just by connecting wires.

All it takes is your permission, by sharing your code with the community.

In this chapter we will create the simplest possible device: three C
functions that print a value to the screen — an integer, a floating-point
number, and a text. **Each function becomes an independent block** inside
the same device.

---

## 1.2 Creating the device in the panel

![Create with wizard](/00_howto/01_device/img/c99-01-new-device.png)

1. Log in to the control panel and open the **Devices & Templates**
   section.
2. Click **New Project** and choose **Create with wizard**.
3. Give the device a name. In this chapter we will use `stdOut`.
4. Choose **C99** as the programming language.

![Create with wizard](/00_howto/01_device/img/c99-02-new-device.png)

Once you confirm, the IDE opens the device's code editor, still empty:

![Empty code editor](/00_howto/01_device/img/c99-03-new-device.png)

Take a look at the top bar. It will stay with us throughout the chapter:

| Element           | What it does                                                  |
|-------------------|---------------------------------------------------------------|
| **Editor**        | Where you write the device's C code.                          |
| **Wizard**        | Configures what Parse found (names, icons, ports).            |
| **Preview**       | Shows how the graphical blocks will look in the IDE.          |
| **Debug**         | Technical parse information, for troubleshooting.             |
| **Parse**         | Reads your code and discovers functions and parameters.       |
| **Save**          | Stores a version of the device (v1, v2, ...).                 |
| **Live analysis** | When checked, analyzes the code as you type.                  |

---

## 1.3 Writing the C code

Type (or paste) the code below into the **Editor**:

```c
#include <stdio.h>

// print_int writes a single integer to standard output, followed by a
// newline. Host targets only (PC/Linux): stdout is a real stream here.
// On microcontrollers there is no stdout — the serial/UART path is used
// instead, so this function is not portable to embedded targets.
void print_int(int value) {
    printf("%d\n", value);
}

// print_float writes a single floating-point number to standard output,
// followed by a newline. The %f conversion expects a double; a float
// argument is promoted to double automatically by the C varargs rules,
// so passing `value` directly is correct and portable. Host targets
// only (PC/Linux) — see print_int for the embedded-target caveat.
void print_float(float value) {
    printf("%f\n", value);
}

// print_string writes a NUL-terminated string to standard output,
// followed by a newline. A NULL pointer is printed as "(null)" instead
// of being passed to printf — printf("%s", NULL) is undefined behavior
// in C, so the guard keeps the block safe no matter what the maker
// wires into it. Host targets only (PC/Linux) — see print_int.
void print_string(const char *value) {
    if (value == NULL) {
        printf("(null)\n");
        return;
    }
    printf("%s\n", value);
}
```

![Editor with C code](/00_howto/01_device/img/c99-04-new-device.png)

Four important things happen in this code, and it is worth understanding
each of them before moving on:

**Each function becomes a block.** The file has three public functions —
`print_int`, `print_float`, and `print_string` — so the `stdOut` device
offers three blocks to the user. A device is like a toolbox: it groups
related blocks together.

**The parameter becomes a port.** Each function's parameter becomes an
input port on the corresponding block — that is where the user connects
the wire carrying the value to be printed. The parameter's type defines
the wire color and which other ports it can be connected to:

| C type         | Wire type in the IDE | Note                                                                                                                                              |
|----------------|----------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------|
| `int`          | `INT`                | Integer number.                                                                                                                                   |
| `float`        | `FLOAT`              | Floating-point number. The bit width (32/64) is decided by the target board at code-generation time — you don't need to worry about it here.      |
| `const char *` | `STRING`             | Text. In C, a string is a pointer to characters; the `const` states that the function only reads the text, never modifies it.                     |

**The comment becomes documentation.** The comment block immediately
above each function is captured by Parse and shown to the user inside the
IDE. Write it with the person who will *use* the block in mind, not the
person who will read the code.
In section 1.8 we will see how to write the documentation in multiple
languages.

**Defensive code protects the user.** Notice the `NULL` guard in
`print_string`: in C, passing a null pointer to `printf("%s", ...)` is
undefined behavior — the program may crash. Since the specialist has no
control over what the maker will wire into the port, the block prints
`(null)` instead of taking the risk. Defensive blocks are the mark of a
good specialist: the user trusts that the little box will never bring
their program down.

> **Tip:** this device uses `printf`, which only exists on targets with
> an operating system (PC/Linux). On a microcontroller there is no
> `stdout` — output goes through the serial/UART. That is why the
> comments warn that these functions are not portable to embedded
> targets. Documenting limitations is part of the job: the user of the
> block trusts what you wrote.

---

## 1.4 Parse: turning code into blocks

With the code ready, click **Parse** (notice the reminder on the right
side of the screen: *Click Parse to visualise*).

Parse reads the file and discovers the three functions and their
parameters. Then the **Wizard** opens a configuration window for each
item found. We will configure `print_int` in detail — after that, just
repeat the same process for `print_float` and `print_string`.

The first window is the function's:

![Wizard](/00_howto/01_device/img/c99-05-new-device.png)

| Field                | What to fill in                                                                                                                         |
|----------------------|-------------------------------------------------------------------------------------------------------------------------------------------|
| **ID**               | Internal identifier, derived from the function name. Cannot be changed.                                                                 |
| **Label**            | The name written on the block, on the stage. It can be friendlier than the function name.                                               |
| **Icon**             | The drawing shown at the top of the block. Click an icon in the grid or type its name (they are FontAwesome icons, like `bolt` or `gear`). |
| **Callback handler** | Leave it as **— Not a callback handler —**. Callbacks are an advanced feature, covered in another chapter.                              |

Choose an icon that helps the user recognize the block from a distance —
for printing functions, something like a terminal or an outgoing arrow
works well. You can use the same icon for all three blocks or vary them
(for example, `hashtag` for the integer, `percent` for the float,
`quote-right` for the string). Then click **Save** in the window.

![print_int function wizard](/00_howto/01_device/img/c99-06-new-device.png)

> The **Add help** button, between Cancel and Save, creates the device's
> manual. We will use it in section 1.8 — for now, keep going.

---

## 1.5 Configuring the ports

Next, the Wizard opens the port configuration for each function. The
window title shows the full path — for example,
**Port · print_int · input · value** (port *value*, input, of the
*print_int* function).

The most important field here is **Connection**, and it has two options:

- **Optional — port may be left unwired**: the user may leave the port
  without a wire. Use it when the function has a reasonable default value
  or when the parameter is a fine-tuning knob.
- **Mandatory — port must be wired**: the user is required to connect a
  wire to this port. If they don't, the IDE reports an error before
  generating code.

For the three ports of this device, choose **Mandatory**: printing
"nothing" makes no sense, so each port must always receive a value.

The **Comment** field is also required. It is the port's description: it
appears when the user hovers over the pin in the IDE and becomes a
comment in the generated code. A short sentence is enough — describe what
the port *carries*, for example:

| Port                   | Suggested comment               |
|------------------------|---------------------------------|
| `print_int · value`    | `Integer value to print`        |
| `print_float · value`  | `Floating-point value to print` |
| `print_string · value` | `Text to print`                 |

![Port configured with a comment](/00_howto/01_device/img/c99-07-new-device.png)

Click **Save** in each port's window. By the end, you will have
configured three functions and three ports.

---

### 1.5.1 Scalar pointers: probe wires

When a function **returns** a pointer to a scalar (`int32_t *get_buffer()`)
or takes one as a **plain input** (`const int32_t *data` without
`direction:out`), the port exposes a **pointer wire**: same colour as the
base family, drawn **dashed**. Every integer width collapses to the `int*`
family token; `float`/`double` become `float*`.

Pointer wires connect **only to the Debug (Print) devices** — they are
probe wires. The generated code dereferences automatically, and a null
pointer prints `null pointer` instead of crashing. Three conventions are
preserved: `char *` is still a C **string value** (no pointer wire),
struct pointers still travel as **handles** (the resource-chain idiom),
and `direction:out` pointers still split into value outputs.

## 1.6 Checking the Preview

Open the **Preview** tab. It shows the blocks exactly as they will appear
on the stage:

![Parser](/00_howto/01_device/img/c99-08-new-device.png)

Check, on each block:

1. **The icon and the label** are as you configured them in the Wizard.
2. **The `value` port** shows the correct type next to it (`int`,
   `float32`, `string`).
3. **The pin color** indicates whether it is required — the legend below
   the blocks shows: **green** pin = *mandatory*, **hollow blue** pin =
   *optional*.

Hover over the pins (*Hover pins for details*) to see the comments you
wrote in the previous section.

---

## 1.7 Saving the first version

Back at the top bar, click **Save**. The device gets version **v1**,
which now appears next to its name in the navigation bar.

From the second version onwards, the **Diff** button lets you compare
what changed between versions — useful when you evolve the device without
breaking the projects of people who already use it.

---

## 1.8 Writing the device's manual (recommended!)

Your device already works, but the most important thing for its users is
still missing: **the manual**.

If you don't write a manual, the IDE shows the user only the source-code
comment — which is better than nothing, but mixes in technical details
the end user doesn't need to see.

This is the standard we are aiming for. To create the manual:

1. Go back to the **Editor** and run **Parse** (the help manager needs
   the parse result to know which files it can create).
2. Open the **Wizard**, enter a function's window, and click
   **Add help**.
3. The help file manager opens with the creation window already
   prepared. Choose:
	- **What to document:** *main menu text* creates the device's general
	  text (what appears in the IDE menu); a function name (e.g.
	  `print_int`) creates the documentation for that specific block.
	- **The language:** `en`, `pt-br`, `es`, `fr`, `de`, `zh-cn`, or `ja`.
4. Write the content in Markdown and save.
5. Repeat for each block and each language you want to cover.

![Documentation - step 1](/00_howto/01_device/img/c99-09-new-device.png)

![Documentation - step 2](/00_howto/01_device/img/c99-10-new-device.png)

![Documentation - step 3](/00_howto/01_device/img/c99-11-new-device.png)

The files follow the `<name>.<language>.md` convention. For the complete
`stdOut`, in English and Portuguese, the set looks like this:

```
readme.en.md          readme.pt-br.md          ← menu text
print_int.en.md       print_int.pt-br.md       ← int block manual
print_float.en.md     print_float.pt-br.md     ← float block manual
print_string.en.md    print_string.pt-br.md    ← string block manual
```

The IDE automatically picks the file matching the user's language.

**Suggested structure for a block's manual** (the same one used by the
native devices):

```markdown
# print_int — Print integer

Prints an integer number to the screen, followed by a line break.

## Ports

| Port  | Direction | Type | Description           |
|-------|-----------|------|-----------------------|
| value | Input     | int  | Value to be printed   |

## Tip

This device only works on PC/Linux targets. On microcontrollers, use a
serial output device.
```

---

## 1.9 Using the device in a project

Now let's use `stdOut` for real. Create (or open) a **C99** project in
the IDE — notice the `C99` indicator in the top-right corner of the
stage.

Open the **Menu** (the hexagon in the corner of the stage) and navigate
to **My Items** in the sidebar. That is where the devices *you* created
live. Inside `stdOut` you will find the three blocks:

![Menu - step 1](/00_howto/01_device/img/c99-12-new-device.png)

![Menu - step 2](/00_howto/01_device/img/c99-13-new-device.png)

Click `print_int` and then **+ Place on stage**. The block appears on the
stage, ready to receive wires.

To test it, build this small program: "take the element at index 3 of a
list of numbers and print it to the screen". You will need three more
devices, all native to the IDE:

- **Const → Array Int** (`constArrayInt`): the list `{0,1,2,3,4,5,6,...}`.
- **Const → Int** (`constInt`): the constant `3`, the desired index.
- **Index** (`indexInt`): takes the list and the index, returns the
  element.

Connect the wires like this:

1. Output of `constArrayInt_0` → first input of `indexInt_0` (the list).
2. Output of `constInt_0` → second input of `indexInt_0` (the index).
3. Output of `indexInt_0` → **value** port of your `print_int_1`.

![ide](/00_howto/01_device/img/c99-14-new-device.png)

Notice that a wire only "accepts" connecting ports of compatible types —
the list (`[]INT`, thicker wire) only fits the list port, and a plain
`INT` only fits `INT` ports. The IDE prevents wrong connections before
you even generate code.

> **Try it:** swap `print_int` for `print_string` and try to connect the
> `INT` output of the index into its port. The IDE refuses the
> connection — the types don't match. This is how IoTMaker catches, at
> assembly time, the errors that in C would only show up at compile time
> (or worse, at run time).

---

## 1.10 Generating the source code

![code export - 1](/00_howto/01_device/img/c99-15-new-device.png)

![code export - 2](/00_howto/01_device/img/c99-16-new-device.png)

![code export - 3](/00_howto/01_device/img/c99-17-new-device.png)

With the program assembled, open **Menu → Export** and choose the C code
export. The IDE asks which board you want to generate code for — for this
test, choose **PC** so you can compile and run it on your computer
(remember: `printf` does not exist on microcontrollers).

IoTMaker generates a complete C99 project, as a zip file, containing:

- the code of your `stdOut` device, exactly as you wrote it;
- the code of the native devices used (constants, index);
- a `main` that wires everything together in the right order, following
  the wires on the stage.

Unzip, compile, and run. If everything went well, the program prints `3`
to the screen — the element at index 3 of the list `{0,1,2,3,...}`.

**Congratulations: you created your first C99 device and used it to
generate a real program.** 🎉

---

## 1.11 Common errors

| Symptom                                                   | Likely cause                                                                | Solution                                                                                        |
|-----------------------------------------------------------|------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------|
| Parse does not find a function                            | The function signature is incomplete or the file has a syntax error         | Check the code in the Editor; **Live analysis** underlines problems as you type                 |
| The **Add help** button says "Run Parse first"            | The help manager needs the parse result                                     | Run **Parse** and try again                                                                     |
| The blocks do not appear in **My Items**                  | The device was not saved (**Save**) after Parse                             | Go back to the device editor and save the version                                               |
| The IDE reports a mandatory port without a wire           | A **Mandatory** port was left unconnected on the stage                      | Connect a wire to the indicated port, or make the port **Optional** in the device's next version |
| A wire won't connect to a port                            | The types are not compatible (e.g. `INT` into a `STRING` port)              | Use a conversion block or the right-typed port — in `stdOut`, each type has its own block       |
| The block documentation looks "mixed" (two languages)     | The device has no manual files; the IDE fell back to the source-code comment | Create the manual files (section 1.8), one per language                                         |

---

## Important tip

In the `export` > `image` menu, you can save an image of your program —
and, as long as this image is not edited, it carries a copy of the source
inside it: just drag it onto the IDE's stage to load it back.

However, it does not carry the devices you created; those must exist in
the user's library.
