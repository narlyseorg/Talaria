package monitor

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Foundation -lobjc
#include <objc/runtime.h>
#include <objc/message.h>



static long get_thermal_state() {
    Class cls = objc_getClass("NSProcessInfo");
    SEL selPI = sel_registerName("processInfo");
    SEL selTS = sel_registerName("thermalState");

    id pi = ((id (*)(id, SEL))objc_msgSend)((id)cls, selPI);
    long state = ((long (*)(id, SEL))objc_msgSend)(pi, selTS);
    return state;
}
*/
import "C"

type ThermalMetrics struct {
	ThermalState string `json:"thermal_state"` // "Nominal", "Fair", "Serious", "Critical"
	CPUTemp      int    `json:"cpu_temp"`      // Degree Celsius (if available)
}

var thermalStates = [4]string{"Nominal", "Fair", "Serious", "Critical"}

func GetThermal() ThermalMetrics {
	state := int(C.get_thermal_state())

	m := ThermalMetrics{}
	if state >= 0 && state < len(thermalStates) {
		m.ThermalState = thermalStates[state]
	} else {
		m.ThermalState = "Unknown"
	}
	return m
}
