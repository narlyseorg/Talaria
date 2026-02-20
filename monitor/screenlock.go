package monitor

/*
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation
#include <CoreGraphics/CoreGraphics.h>
#include <CoreFoundation/CoreFoundation.h>



static int is_screen_locked() {
    CFDictionaryRef dict = CGSessionCopyCurrentDictionary();
    if (!dict) return 0;


    const void *lockedVal = CFDictionaryGetValue(dict, CFSTR("CGSSessionScreenIsLocked"));
    int locked = 0;
    if (lockedVal) {
        CFTypeID type = CFGetTypeID(lockedVal);
        if (type == CFBooleanGetTypeID()) {
            locked = CFBooleanGetValue((CFBooleanRef)lockedVal) ? 1 : 0;
        }
    }

    CFRelease(dict);
    return locked;
}
*/
import "C"

func IsScreenLocked() bool {
	return C.is_screen_locked() == 1
}
