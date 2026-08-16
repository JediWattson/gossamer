#import <AppKit/AppKit.h>
#import <CoreGraphics/CoreGraphics.h>

#include "cocoa_darwin.h"

#include <stdlib.h>
#include <string.h>
#include <math.h>

enum {
  GOSSAMER_EVENT_NONE = 0,
  GOSSAMER_EVENT_CLOSE = 1,
  GOSSAMER_EVENT_RESIZE = 2,
  GOSSAMER_EVENT_POINTER_MOVE = 3,
  GOSSAMER_EVENT_POINTER_DOWN = 4,
  GOSSAMER_EVENT_POINTER_UP = 5,
  GOSSAMER_EVENT_SCROLL = 6,
  GOSSAMER_EVENT_KEY_DOWN = 7,
  GOSSAMER_EVENT_KEY_UP = 8,
  GOSSAMER_EVENT_FOCUS = 9,
  GOSSAMER_EVENT_BLUR = 10,
};

struct gossamer_cocoa_window;

@interface GossamerView : NSView {
 @public
  uint8_t *_pixels;
  size_t _width;
  size_t _height;
  size_t _stride;
}
- (BOOL)setPixels:(const uint8_t *)pixels
            width:(size_t)width
           height:(size_t)height
           stride:(size_t)stride;
@end

@interface GossamerWindowDelegate : NSObject <NSWindowDelegate> {
 @public
  struct gossamer_cocoa_window *_state;
}
@end

struct gossamer_cocoa_window {
  NSWindow *window;
  GossamerView *view;
  GossamerWindowDelegate *delegate;
  int closed;
  int focus_change;
  int last_width;
  int last_height;
};

static void gossamer_set_error(char **error, NSString *message) {
  if (error == NULL)
    return;
  const char *source = [message UTF8String];
  *error = source == NULL ? NULL : strdup(source);
}

static void gossamer_copy_string(char *destination, size_t capacity,
                                 NSString *source) {
  if (capacity == 0)
    return;
  destination[0] = '\0';
  if (source == nil)
    return;
  const char *utf8 = [source UTF8String];
  if (utf8 == NULL)
    return;
  strlcpy(destination, utf8, capacity);
}

@implementation GossamerView
- (BOOL)acceptsFirstResponder {
  return YES;
}

- (void)dealloc {
  free(_pixels);
  [super dealloc];
}

- (BOOL)setPixels:(const uint8_t *)pixels
            width:(size_t)width
           height:(size_t)height
           stride:(size_t)stride {
  if (height != 0 && stride > SIZE_MAX / height)
    return NO;
  size_t size = stride * height;
  uint8_t *copy = size == 0 ? NULL : malloc(size);
  if (size != 0 && copy == NULL)
    return NO;
  if (size != 0)
    memcpy(copy, pixels, size);
  free(_pixels);
  _pixels = copy;
  _width = width;
  _height = height;
  _stride = stride;
  [self setNeedsDisplay:YES];
  return YES;
}

- (void)drawRect:(NSRect)dirtyRect {
  (void)dirtyRect;
  [[NSColor whiteColor] setFill];
  NSRectFill([self bounds]);
  if (_pixels == NULL || _width == 0 || _height == 0)
    return;
  CGColorSpaceRef colorSpace = CGColorSpaceCreateDeviceRGB();
  CGDataProviderRef provider =
      CGDataProviderCreateWithData(NULL, _pixels, _stride * _height, NULL);
  if (colorSpace == NULL || provider == NULL) {
    if (provider != NULL)
      CGDataProviderRelease(provider);
    if (colorSpace != NULL)
      CGColorSpaceRelease(colorSpace);
    return;
  }
  CGImageRef image = CGImageCreate(
      _width, _height, 8, 32, _stride, colorSpace,
      kCGBitmapByteOrder32Big | kCGImageAlphaPremultipliedLast, provider, NULL,
      false, kCGRenderingIntentDefault);
  if (image == NULL) {
    CGDataProviderRelease(provider);
    CGColorSpaceRelease(colorSpace);
    return;
  }
  CGContextRef context = [[NSGraphicsContext currentContext] CGContext];
  CGRect bounds = NSRectToCGRect([self bounds]);
  CGContextSaveGState(context);
  CGContextTranslateCTM(context, 0, bounds.size.height);
  CGContextScaleCTM(context, 1, -1);
  CGContextDrawImage(context, bounds, image);
  CGContextRestoreGState(context);
  CGImageRelease(image);
  CGDataProviderRelease(provider);
  CGColorSpaceRelease(colorSpace);
}
@end

@implementation GossamerWindowDelegate
- (BOOL)windowShouldClose:(id)sender {
  (void)sender;
  if (_state != NULL)
    _state->closed = 1;
  return NO;
}
- (void)windowDidBecomeKey:(NSNotification *)notification {
  (void)notification;
  if (_state != NULL)
    _state->focus_change = 1;
}
- (void)windowDidResignKey:(NSNotification *)notification {
  (void)notification;
  if (_state != NULL)
    _state->focus_change = -1;
}
@end

gossamer_cocoa_window *gossamer_cocoa_open(const char *title, int width,
                                            int height, char **error) {
  @autoreleasepool {
    if (error != NULL)
      *error = NULL;
    if (width <= 0 || height <= 0) {
      gossamer_set_error(error, @"invalid initial window size");
      return NULL;
    }
    if (![NSThread isMainThread]) {
      gossamer_set_error(error, @"AppKit backend must open on the main thread");
      return NULL;
    }
    [NSApplication sharedApplication];
    [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
    [NSApp finishLaunching];
    gossamer_cocoa_window *state = calloc(1, sizeof(*state));
    if (state == NULL) {
      gossamer_set_error(error, @"allocating native window state failed");
      return NULL;
    }
    NSRect rectangle = NSMakeRect(0, 0, width, height);
    NSWindowStyleMask style = NSWindowStyleMaskTitled |
                              NSWindowStyleMaskClosable |
                              NSWindowStyleMaskMiniaturizable |
                              NSWindowStyleMaskResizable;
    state->window = [[NSWindow alloc] initWithContentRect:rectangle
                                                styleMask:style
                                                  backing:NSBackingStoreBuffered
                                                    defer:NO];
    state->view = [[GossamerView alloc] initWithFrame:rectangle];
    state->delegate = [[GossamerWindowDelegate alloc] init];
    state->delegate->_state = state;
    [state->window setDelegate:state->delegate];
    [state->window setReleasedWhenClosed:NO];
    [state->window setContentView:state->view];
    [state->window setAcceptsMouseMovedEvents:YES];
    NSString *windowTitle = title == NULL
                                ? @"Gossamer"
                                : [NSString stringWithUTF8String:title];
    [state->window setTitle:windowTitle == nil ? @"Gossamer" : windowTitle];
    [state->window center];
    [state->window makeKeyAndOrderFront:nil];
    [state->window makeFirstResponder:state->view];
    [NSApp activateIgnoringOtherApps:YES];
    state->last_width = width;
    state->last_height = height;
    return state;
  }
}

static void gossamer_event_modifiers(NSEvent *native,
                                     gossamer_cocoa_event *event) {
  NSEventModifierFlags flags = [native modifierFlags];
  event->alt = (flags & NSEventModifierFlagOption) != 0;
  event->control = (flags & NSEventModifierFlagControl) != 0;
  event->command = (flags & NSEventModifierFlagCommand) != 0;
  event->shift = (flags & NSEventModifierFlagShift) != 0;
}

static void gossamer_event_location(gossamer_cocoa_window *state,
                                    NSEvent *native,
                                    gossamer_cocoa_event *event) {
  NSPoint point = [state->view convertPoint:[native locationInWindow]
                                   fromView:nil];
  event->x = point.x;
  event->y = NSHeight([state->view bounds]) - point.y;
  event->button = (int32_t)[native buttonNumber];
  event->buttons = (uint32_t)[NSEvent pressedMouseButtons];
}

int gossamer_cocoa_next_event(gossamer_cocoa_window *state,
                              gossamer_cocoa_event *event,
                              double timeout_seconds, char **error) {
  @autoreleasepool {
    if (error != NULL)
      *error = NULL;
    if (state == NULL || event == NULL) {
      gossamer_set_error(error, @"native window or event is null");
      return 0;
    }
    memset(event, 0, sizeof(*event));
    if (state->closed) {
      event->kind = GOSSAMER_EVENT_CLOSE;
      return 1;
    }
    NSSize size = [state->view bounds].size;
    int width = (int)llround(size.width);
    int height = (int)llround(size.height);
    if (width > 0 && height > 0 &&
        (width != state->last_width || height != state->last_height)) {
      state->last_width = width;
      state->last_height = height;
      event->kind = GOSSAMER_EVENT_RESIZE;
      event->width = width;
      event->height = height;
      return 1;
    }
    if (state->focus_change != 0) {
      event->kind = state->focus_change > 0 ? GOSSAMER_EVENT_FOCUS
                                            : GOSSAMER_EVENT_BLUR;
      state->focus_change = 0;
      return 1;
    }
    NSDate *deadline =
        [NSDate dateWithTimeIntervalSinceNow:MAX(0.0, timeout_seconds)];
    NSEvent *native = [NSApp nextEventMatchingMask:NSEventMaskAny
                                         untilDate:deadline
                                            inMode:NSDefaultRunLoopMode
                                           dequeue:YES];
    if (native == nil) {
      event->kind = GOSSAMER_EVENT_NONE;
      return 1;
    }
    NSEventType type = [native type];
    gossamer_event_modifiers(native, event);
    switch (type) {
    case NSEventTypeMouseMoved:
    case NSEventTypeLeftMouseDragged:
    case NSEventTypeRightMouseDragged:
    case NSEventTypeOtherMouseDragged:
      event->kind = GOSSAMER_EVENT_POINTER_MOVE;
      gossamer_event_location(state, native, event);
      break;
    case NSEventTypeLeftMouseDown:
    case NSEventTypeRightMouseDown:
    case NSEventTypeOtherMouseDown:
      event->kind = GOSSAMER_EVENT_POINTER_DOWN;
      gossamer_event_location(state, native, event);
      break;
    case NSEventTypeLeftMouseUp:
    case NSEventTypeRightMouseUp:
    case NSEventTypeOtherMouseUp:
      event->kind = GOSSAMER_EVENT_POINTER_UP;
      gossamer_event_location(state, native, event);
      break;
    case NSEventTypeScrollWheel:
      event->kind = GOSSAMER_EVENT_SCROLL;
      event->delta_x = -[native scrollingDeltaX];
      event->delta_y = -[native scrollingDeltaY];
      break;
    case NSEventTypeKeyDown: {
      event->kind = GOSSAMER_EVENT_KEY_DOWN;
      event->key_code = [native keyCode];
      event->repeat = [native isARepeat] ? 1 : 0;
      gossamer_copy_string(event->key, sizeof(event->key),
                           [native charactersIgnoringModifiers]);
      NSString *characters = [native characters];
      if (!event->command && !event->control && [characters length] > 0) {
        unichar first = [characters characterAtIndex:0];
        if (![[NSCharacterSet controlCharacterSet] characterIsMember:first])
          gossamer_copy_string(event->text, sizeof(event->text), characters);
      }
      break;
    }
    case NSEventTypeKeyUp:
      event->kind = GOSSAMER_EVENT_KEY_UP;
      event->key_code = [native keyCode];
      gossamer_copy_string(event->key, sizeof(event->key),
                           [native charactersIgnoringModifiers]);
      break;
    default:
      event->kind = GOSSAMER_EVENT_NONE;
      break;
    }
    [NSApp sendEvent:native];
    [NSApp updateWindows];
    if (state->closed)
      event->kind = GOSSAMER_EVENT_CLOSE;
    return 1;
  }
}

int gossamer_cocoa_present(gossamer_cocoa_window *state,
                           const uint8_t *pixels, int width, int height,
                           int stride, char **error) {
  @autoreleasepool {
    if (error != NULL)
      *error = NULL;
    if (state == NULL || pixels == NULL || width <= 0 || height <= 0 ||
        stride < width * 4) {
      gossamer_set_error(error, @"invalid native frame");
      return 0;
    }
    if (![state->view setPixels:pixels
                          width:(size_t)width
                         height:(size_t)height
                         stride:(size_t)stride]) {
      gossamer_set_error(error, @"copying native frame failed");
      return 0;
    }
    [state->window displayIfNeeded];
    return 1;
  }
}

void gossamer_cocoa_close(gossamer_cocoa_window *state) {
  if (state == NULL)
    return;
  @autoreleasepool {
    state->delegate->_state = NULL;
    [state->window setDelegate:nil];
    [state->window orderOut:nil];
    [state->window close];
    [state->delegate release];
    [state->view release];
    [state->window release];
    free(state);
  }
}
