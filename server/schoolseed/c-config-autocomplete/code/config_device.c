// config_device.c — mission: give the maker autocomplete.
//
// This device receives a YAML configuration and prints it. It parses
// already — but the maker's editor knows nothing about YAML yet. Your
// mission (see the panel): fill the dictionary, then point the cfg port
// at it using the port editor. No directives to memorize — the modal
// writes them for you.

#include <stdint.h>
#include <stdio.h>

// Receives the application configuration and echoes it.
//
// label:App config.
// icon:sliders.
// min-target:posix.
void app_config(
    // The configuration authored by the maker.
    // connection:mandatory.
    // slice:n.
    const uint8_t *cfg,
    unsigned long n
) {
    printf("[config] %lu byte(s) received:\n", n);
    fwrite(cfg, 1, (size_t)n, stdout);
    printf("\n[config] end.\n");
}
