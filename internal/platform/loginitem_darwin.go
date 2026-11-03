//go:build darwin

package platform

/*
#cgo CFLAGS: -x objective-c -mmacosx-version-min=13.0
#cgo LDFLAGS: -framework Foundation -framework ServiceManagement
#import <Foundation/Foundation.h>
#import <ServiceManagement/ServiceManagement.h>
#include <stdlib.h>
#include <string.h>

static int visorAppServiceStatus(void) {
	return (int)[SMAppService mainAppService].status;
}

// Returns NULL on success, else a malloc'd message the caller frees.
static char *visorAppServiceSet(int on) {
	NSError *err = nil;
	BOOL ok = on ? [[SMAppService mainAppService] registerAndReturnError:&err]
	             : [[SMAppService mainAppService] unregisterAndReturnError:&err];
	if (ok) return NULL;
	return strdup(err ? err.localizedDescription.UTF8String : "unknown error");
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

// AppService is a LoginItem through SMAppService.mainAppService (macOS 13+),
// the system's own Login Items list. It may refuse an unsigned app.
type AppService struct{ moved func() bool }

// NewAppService returns the SMAppService login item.
func NewAppService(moved func() bool) *AppService { return &AppService{moved: moved} }

func (a *AppService) Status() LoginItemStatus {
	if BundlePath() == "" {
		return LoginUnsupported
	}
	st := appServiceStatus(int(C.visorAppServiceStatus()))
	if st == LoginDisabled && !a.moved() {
		return LoginNotMoved
	}
	return st
}

func (a *AppService) Set(on bool) error {
	if on && BundlePath() == "" {
		return ErrLoginUnsupported
	}
	if on && !a.moved() {
		return ErrNotMoved
	}
	var flag C.int
	if on {
		flag = 1
	}
	if msg := C.visorAppServiceSet(flag); msg != nil {
		defer C.free(unsafe.Pointer(msg))
		return errors.New(C.GoString(msg))
	}
	return nil
}
