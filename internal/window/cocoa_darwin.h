#ifndef GOSSAMER_COCOA_DARWIN_H
#define GOSSAMER_COCOA_DARWIN_H

#include <stdint.h>

typedef struct gossamer_cocoa_window gossamer_cocoa_window;

typedef struct {
  int32_t kind;
  int32_t width;
  int32_t height;
  double x;
  double y;
  double delta_x;
  double delta_y;
  int32_t button;
  uint32_t buttons;
  uint16_t key_code;
  uint8_t repeat;
  uint8_t alt;
  uint8_t control;
  uint8_t command;
  uint8_t shift;
  uint8_t composing;
  char key[64];
  char text[128];
} gossamer_cocoa_event;

gossamer_cocoa_window *gossamer_cocoa_open(const char *title, int width,
                                            int height, char **error);
int gossamer_cocoa_next_event(gossamer_cocoa_window *window,
                              gossamer_cocoa_event *event,
                              double timeout_seconds, char **error);
int gossamer_cocoa_present(gossamer_cocoa_window *window,
                           const uint8_t *pixels, int width, int height,
                           int stride, char **error);
void gossamer_cocoa_close(gossamer_cocoa_window *window);
int gossamer_cocoa_read_clipboard(char **value, char **error);
int gossamer_cocoa_write_clipboard(const char *value, char **error);

#endif
