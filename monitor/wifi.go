package monitor

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework CoreWLAN
#include <objc/runtime.h>
#include <objc/message.h>
#include <string.h>
#include <stdbool.h>


static bool responds(id obj, SEL sel) {
    return ((bool (*)(id, SEL, SEL))objc_msgSend)(
        obj, sel_registerName("respondsToSelector:"), sel);
}





static const char* cw_get_ssid() {
    Class cls = objc_getClass("CWWiFiClient");
    if (!cls) return "";

    id client = ((id (*)(id, SEL))objc_msgSend)(
        (id)cls, sel_registerName("sharedWiFiClient"));
    if (!client) return "";

    id iface = ((id (*)(id, SEL))objc_msgSend)(
        client, sel_registerName("interface"));
    if (!iface) return "";


    SEL ssidSel = sel_registerName("ssid");
    if (!responds(iface, ssidSel)) return "";

    id ssid = ((id (*)(id, SEL))objc_msgSend)(iface, ssidSel);
    if (!ssid) return "";

    const char* s = ((const char* (*)(id, SEL))objc_msgSend)(
        ssid, sel_registerName("UTF8String"));
    return s ? s : "";
}



static const char* cw_interface_name() {
    Class cls = objc_getClass("CWWiFiClient");
    if (!cls) return "";

    id client = ((id (*)(id, SEL))objc_msgSend)(
        (id)cls, sel_registerName("sharedWiFiClient"));
    if (!client) return "";

    id iface = ((id (*)(id, SEL))objc_msgSend)(
        client, sel_registerName("interface"));
    if (!iface) return "";

    SEL nameSel = sel_registerName("interfaceName");
    if (!responds(iface, nameSel)) return "";

    id name = ((id (*)(id, SEL))objc_msgSend)(iface, nameSel);
    if (!name) return "";

    const char* s = ((const char* (*)(id, SEL))objc_msgSend)(
        name, sel_registerName("UTF8String"));
    return s ? s : "";
}
*/
import "C"

func GetWiFiSSID() string {
	return C.GoString(C.cw_get_ssid())
}

func GetWiFiInterfaceName() string {
	return C.GoString(C.cw_interface_name())
}
