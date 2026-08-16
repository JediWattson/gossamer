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
  GOSSAMER_EVENT_TEXT_INPUT = 11,
  GOSSAMER_EVENT_COMPOSITION_START = 12,
  GOSSAMER_EVENT_COMPOSITION_UPDATE = 13,
  GOSSAMER_EVENT_COMPOSITION_END = 14,
};

struct gossamer_cocoa_window;
static void gossamer_queue_text_event(struct gossamer_cocoa_window *state,
                                      int kind, NSString *text,
                                      int composing);

@interface GossamerView : NSView <NSTextInputClient> {
 @public
  uint8_t *_pixels;
  size_t _width;
  size_t _height;
  size_t _stride;
  struct gossamer_cocoa_window *_state;
  NSMutableString *_markedText;
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
  gossamer_cocoa_event pending_events[8];
  unsigned int pending_head;
  unsigned int pending_count;
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

static void gossamer_queue_text_event(gossamer_cocoa_window *state, int kind,
                                      NSString *text, int composing) {
  if (state == NULL || state->pending_count >= 8)
    return;
  unsigned int index = (state->pending_head + state->pending_count) % 8;
  gossamer_cocoa_event *event = &state->pending_events[index];
  memset(event, 0, sizeof(*event));
  event->kind = kind;
  event->composing = composing ? 1 : 0;
  gossamer_copy_string(event->text, sizeof(event->text), text);
  state->pending_count++;
}

static int gossamer_pop_text_event(gossamer_cocoa_window *state,
                                   gossamer_cocoa_event *event) {
  if (state == NULL || event == NULL || state->pending_count == 0)
    return 0;
  *event = state->pending_events[state->pending_head];
  state->pending_head = (state->pending_head + 1) % 8;
  state->pending_count--;
  return 1;
}

static void gossamer_draw_frame(CGContextRef context, CGRect bounds,
                                CGImageRef image) {
  CGContextDrawImage(context, bounds, image);
}

@implementation GossamerView
- (BOOL)acceptsFirstResponder {
  return YES;
}

- (void)dealloc {
  free(_pixels);
  [_markedText release];
  [super dealloc];
}

- (void)keyDown:(NSEvent *)event {
  if (![[self inputContext] handleEvent:event])
    [super keyDown:event];
}

- (BOOL)hasMarkedText {
  return _markedText != nil && [_markedText length] != 0;
}

- (NSRange)markedRange {
  if (![self hasMarkedText])
    return NSMakeRange(NSNotFound, 0);
  return NSMakeRange(0, [_markedText length]);
}

- (NSRange)selectedRange {
  return NSMakeRange(0, 0);
}

- (void)setMarkedText:(id)text
         selectedRange:(NSRange)selectedRange
       replacementRange:(NSRange)replacementRange {
  (void)selectedRange;
  (void)replacementRange;
  NSString *plain = [text isKindOfClass:[NSAttributedString class]]
                        ? [(NSAttributedString *)text string]
                        : (NSString *)text;
  BOOL starting = ![self hasMarkedText];
  [_markedText release];
  _markedText = [[NSMutableString alloc] initWithString:plain == nil ? @"" : plain];
  if (starting)
    gossamer_queue_text_event(_state, GOSSAMER_EVENT_COMPOSITION_START, @"", 1);
  gossamer_queue_text_event(_state, GOSSAMER_EVENT_COMPOSITION_UPDATE,
                            _markedText, 1);
}

- (void)unmarkText {
  if (![self hasMarkedText])
    return;
  gossamer_queue_text_event(_state, GOSSAMER_EVENT_COMPOSITION_END,
                            _markedText, 0);
  [_markedText release];
  _markedText = nil;
}

- (void)insertText:(id)text replacementRange:(NSRange)replacementRange {
  (void)replacementRange;
  NSString *plain = [text isKindOfClass:[NSAttributedString class]]
                        ? [(NSAttributedString *)text string]
                        : (NSString *)text;
  if ([self hasMarkedText]) {
    gossamer_queue_text_event(_state, GOSSAMER_EVENT_COMPOSITION_END,
                              plain, 0);
    [_markedText release];
    _markedText = nil;
    return;
  }
  gossamer_queue_text_event(_state, GOSSAMER_EVENT_TEXT_INPUT, plain, 0);
}

- (void)doCommandBySelector:(SEL)selector {
  [[self nextResponder] tryToPerform:selector with:self];
}

- (NSArray<NSAttributedStringKey> *)validAttributesForMarkedText {
  return @[];
}

- (NSAttributedString *)attributedSubstringForProposedRange:(NSRange)range
                                                actualRange:(NSRangePointer)actualRange {
  (void)range;
  if (actualRange != NULL)
    *actualRange = NSMakeRange(NSNotFound, 0);
  return nil;
}

- (NSUInteger)characterIndexForPoint:(NSPoint)point {
  (void)point;
  return NSNotFound;
}

- (NSRect)firstRectForCharacterRange:(NSRange)range
                          actualRange:(NSRangePointer)actualRange {
  if (actualRange != NULL)
    *actualRange = range;
  NSRect local = NSMakeRect(0, 0, 1, 20);
  NSRect windowRect = [self convertRect:local toView:nil];
  return [[self window] convertRectToScreen:windowRect];
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
  [[NSColor colorWithSRGBRed:(8.0 / 255.0)
                       green:(12.0 / 255.0)
                        blue:(17.0 / 255.0)
                       alpha:1.0] setFill];
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
  gossamer_draw_frame(context, bounds, image);
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
    state->view->_state = state;
    state->delegate = [[GossamerWindowDelegate alloc] init];
    state->delegate->_state = state;
    [state->window setDelegate:state->delegate];
    [state->window setReleasedWhenClosed:NO];
    [state->window setAppearance:
        [NSAppearance appearanceNamed:NSAppearanceNameDarkAqua] ];
    [state->window setBackgroundColor:
        [NSColor colorWithSRGBRed:(8.0 / 255.0)
                           green:(12.0 / 255.0)
                            blue:(17.0 / 255.0)
                           alpha:1.0]];
    [state->window setTitlebarAppearsTransparent:YES];
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
    if (gossamer_pop_text_event(state, event))
      return 1;
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
      event->composing = [state->view hasMarkedText] ? 1 : 0;
      gossamer_copy_string(event->key, sizeof(event->key),
                           [native charactersIgnoringModifiers]);
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

int gossamer_cocoa_read_clipboard(char **value, char **error) {
  @autoreleasepool {
    if (value != NULL)
      *value = NULL;
    if (error != NULL)
      *error = NULL;
    if (value == NULL) {
      gossamer_set_error(error, @"clipboard output is null");
      return 0;
    }
    NSString *text = [[NSPasteboard generalPasteboard]
        stringForType:NSPasteboardTypeString];
    const char *utf8 = text == nil ? "" : [text UTF8String];
    *value = strdup(utf8 == NULL ? "" : utf8);
    if (*value == NULL) {
      gossamer_set_error(error, @"allocating clipboard text failed");
      return 0;
    }
    return 1;
  }
}

int gossamer_cocoa_write_clipboard(const char *value, char **error) {
  @autoreleasepool {
    if (error != NULL)
      *error = NULL;
    NSString *text = value == NULL ? @"" : [NSString stringWithUTF8String:value];
    if (text == nil) {
      gossamer_set_error(error, @"clipboard text is not valid UTF-8");
      return 0;
    }
    NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
    [pasteboard clearContents];
    if (![pasteboard setString:text forType:NSPasteboardTypeString]) {
      gossamer_set_error(error, @"writing clipboard text failed");
      return 0;
    }
    return 1;
  }
}

int gossamer_cocoa_presentation_is_top_left(void) {
  @autoreleasepool {
    uint8_t pixels[] = {
        255, 0, 0, 255, 255, 0, 0, 255,
        0, 0, 255, 255, 0, 0, 255, 255,
    };
    CGColorSpaceRef colorSpace = CGColorSpaceCreateDeviceRGB();
    CGDataProviderRef provider = CGDataProviderCreateWithData(
        NULL, pixels, sizeof(pixels), NULL);
    if (colorSpace == NULL || provider == NULL) {
      if (provider != NULL)
        CGDataProviderRelease(provider);
      if (colorSpace != NULL)
        CGColorSpaceRelease(colorSpace);
      return 0;
    }
    CGImageRef image = CGImageCreate(
        2, 2, 8, 32, 8, colorSpace,
        kCGBitmapByteOrder32Big | kCGImageAlphaPremultipliedLast,
        provider, NULL, false, kCGRenderingIntentDefault);
    NSBitmapImageRep *target = [[NSBitmapImageRep alloc]
        initWithBitmapDataPlanes:NULL
                     pixelsWide:2
                     pixelsHigh:2
                  bitsPerSample:8
                samplesPerPixel:4
                       hasAlpha:YES
                       isPlanar:NO
                 colorSpaceName:NSDeviceRGBColorSpace
                    bytesPerRow:8
                   bitsPerPixel:32];
    if (image == NULL || target == nil) {
      [target release];
      if (image != NULL)
        CGImageRelease(image);
      CGDataProviderRelease(provider);
      CGColorSpaceRelease(colorSpace);
      return 0;
    }
    NSGraphicsContext *graphics =
        [NSGraphicsContext graphicsContextWithBitmapImageRep:target];
    [NSGraphicsContext saveGraphicsState];
    [NSGraphicsContext setCurrentContext:graphics];
    gossamer_draw_frame([graphics CGContext], CGRectMake(0, 0, 2, 2), image);
    CGContextFlush([graphics CGContext]);
    [NSGraphicsContext restoreGraphicsState];
    NSColor *firstRow = [[target colorAtX:0 y:0]
        colorUsingColorSpace:[NSColorSpace deviceRGBColorSpace]];
    int topLeft = firstRow != nil &&
                  [firstRow redComponent] > [firstRow blueComponent];
    [target release];
    CGImageRelease(image);
    CGDataProviderRelease(provider);
    CGColorSpaceRelease(colorSpace);
    return topLeft;
  }
}
