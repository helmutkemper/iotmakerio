// blink_led.c — your first IoTMaker device.
//
// This file already works: press Parse and a "Blink LED" card appears.
// Change interval_ms below, Parse again, and watch the card update —
// that is the whole loop: edit, parse, see.

#include <stdio.h>
#include <unistd.h>

// Blinks forever, printing ON / OFF — wire the interval from a ConstInt.
//
// label:Blink LED.
// icon:lightbulb.
// min-target:posix.
void blink_led(
    // Milliseconds between blinks. Try 100. Try 2000.
    // connection:mandatory.
    // doc:Blink interval in milliseconds.
    int interval_ms
) {
    for (;;) {
        printf("LED ON\n");
        usleep((useconds_t)interval_ms * 1000);
        printf("LED OFF\n");
        usleep((useconds_t)interval_ms * 1000);
    }
}
