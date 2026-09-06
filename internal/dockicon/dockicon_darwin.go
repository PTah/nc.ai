//go:build darwin

package dockicon

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

static void dockSetPNG(const void *bytes, int length) {
	@autoreleasepool {
		NSData *data = [NSData dataWithBytes:bytes length:(NSUInteger)length];
		NSImage *image = [[NSImage alloc] initWithData:data];
		if (image == nil) {
			return;
		}
		dispatch_async(dispatch_get_main_queue(), ^{
			[NSApp setApplicationIconImage:image];
		});
	}
}

static void dockResetPNG(const void *bytes, int length) {
	@autoreleasepool {
		NSData *data = [NSData dataWithBytes:bytes length:(NSUInteger)length];
		NSImage *image = [[NSImage alloc] initWithData:data];
		dispatch_async(dispatch_get_main_queue(), ^{
			// Prefer restoring our brand icon; nil falls back to the bundle icon.
			[NSApp setApplicationIconImage:image];
		});
	}
}
*/
import "C"

import (
	"embed"
	"sync"
	"time"
	"unsafe"
)

//go:embed frames/frame1.png frames/frame2.png frames/frame3.png frames/frame4.png frames/frame5.png frames/frame6.png frames/default.png
var frameFS embed.FS

const frameInterval = 100 * time.Millisecond

var (
	animMu   sync.Mutex
	animStop chan struct{}
	frames   [][]byte
	defIcon  []byte
)

func init() {
	names := []string{
		"frames/frame1.png",
		"frames/frame2.png",
		"frames/frame3.png",
		"frames/frame4.png",
		"frames/frame5.png",
		"frames/frame6.png",
	}
	for _, name := range names {
		b, err := frameFS.ReadFile(name)
		if err != nil {
			continue
		}
		frames = append(frames, b)
	}
	if b, err := frameFS.ReadFile("frames/default.png"); err == nil {
		defIcon = b
	}
}

func setPNG(b []byte) {
	if len(b) == 0 {
		return
	}
	C.dockSetPNG(unsafe.Pointer(&b[0]), C.int(len(b)))
}

func resetIcon() {
	if len(defIcon) == 0 {
		return
	}
	C.dockResetPNG(unsafe.Pointer(&defIcon[0]), C.int(len(defIcon)))
}

func startAnim() {
	animMu.Lock()
	defer animMu.Unlock()
	if animStop != nil || len(frames) == 0 {
		return
	}
	stop := make(chan struct{})
	animStop = stop
	go func() {
		ticker := time.NewTicker(frameInterval)
		defer ticker.Stop()
		i := 0
		setPNG(frames[0])
		for {
			select {
			case <-stop:
				resetIcon()
				return
			case <-ticker.C:
				i = (i + 1) % len(frames)
				setPNG(frames[i])
			}
		}
	}()
}

func stopAnim() {
	animMu.Lock()
	defer animMu.Unlock()
	if animStop == nil {
		return
	}
	close(animStop)
	animStop = nil
}
