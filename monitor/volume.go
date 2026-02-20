package monitor

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Foundation
#include <objc/runtime.h>
#include <objc/message.h>
#include <dlfcn.h>
#include <stdint.h>



static id foundation_string_const(const char* symbol) {
    static void* fwk = (void*)0;
    if (!fwk) {
        fwk = dlopen(
            "/System/Library/Frameworks/Foundation.framework/Foundation",
            RTLD_LAZY | RTLD_GLOBAL);
    }
    if (!fwk) return (id)0;
    id* ptr = (id*)dlsym(fwk, symbol);
    return ptr ? *ptr : (id)0;
}


static long long dict_ll(id dict, const char* key_symbol) {
    if (!dict) return -1;
    id key = foundation_string_const(key_symbol);
    if (!key) return -1;
    id num = ((id (*)(id, SEL, id))objc_msgSend)(
        dict, sel_registerName("objectForKey:"), key);
    if (!num) return -1;
    return ((long long (*)(id, SEL))objc_msgSend)(
        num, sel_registerName("longLongValue"));
}






static void cgo_volume_capacities(
        const char* path,
        long long* out_total,
        long long* out_basic,
        long long* out_opport) {
    *out_total  = -1;
    *out_basic  = -1;
    *out_opport = -1;


    Class NSStringClass = objc_getClass("NSString");
    if (!NSStringClass) return;
    id pathStr = ((id (*)(id, SEL, const char*))objc_msgSend)(
        (id)NSStringClass, sel_registerName("stringWithUTF8String:"), path);
    if (!pathStr) return;


    Class NSURLClass = objc_getClass("NSURL");
    if (!NSURLClass) return;
    id url = ((id (*)(id, SEL, id))objc_msgSend)(
        (id)NSURLClass, sel_registerName("fileURLWithPath:"), pathStr);
    if (!url) return;


    id k1 = foundation_string_const("NSURLVolumeTotalCapacityKey");
    id k2 = foundation_string_const("NSURLVolumeAvailableCapacityKey");
    id k3 = foundation_string_const("NSURLVolumeAvailableCapacityForOpportunisticUsageKey");
    if (!k1 || !k2 || !k3) return;

    id keys[3] = {k1, k2, k3};
    Class NSArrayClass = objc_getClass("NSArray");
    if (!NSArrayClass) return;
    id keyArray = ((id (*)(id, SEL, id*, unsigned long))objc_msgSend)(
        (id)NSArrayClass, sel_registerName("arrayWithObjects:count:"),
        keys, (unsigned long)3);
    if (!keyArray) return;


    id dict = ((id (*)(id, SEL, id, id*))objc_msgSend)(
        url, sel_registerName("resourceValuesForKeys:error:"),
        keyArray, (id*)0);
    if (!dict) return;

    *out_total  = dict_ll(dict, "NSURLVolumeTotalCapacityKey");
    *out_basic  = dict_ll(dict, "NSURLVolumeAvailableCapacityKey");
    *out_opport = dict_ll(dict, "NSURLVolumeAvailableCapacityForOpportunisticUsageKey");
}
*/
import "C"

func getFoundationStorageBytes() (total, basic, opportunistic int64) {
	var cTotal, cBasic, cOpport C.longlong
	C.cgo_volume_capacities(
		C.CString("/System/Volumes/Data"),
		&cTotal, &cBasic, &cOpport,
	)
	if cTotal <= 0 {
		return 0, 0, 0
	}
	return int64(cTotal), int64(cBasic), int64(cOpport)
}
