package domain

import "testing"

func TestConnectionReportValidationAndChanges(t *testing.T) {
	p := DeviceProfile{Platform: "darwin", Form: FormDesktop}
	for _, c := range []DeviceConnections{{PhonesPaired: -1}, {PhonesPaired: 17}, {PhonesConnected: 1}, {PhonesPaired: 1, PhonesConnected: -1}} {
		p.Connections = &c
		if p.Validate() == nil {
			t.Fatalf("accepted invalid connections: %+v", c)
		}
	}
	p.Connections = &DeviceConnections{RunnerConnected: true, PhonesPaired: 2, PhonesConnected: 1}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	copy := p
	copy.Connections = &DeviceConnections{RunnerConnected: true, PhonesPaired: 2, PhonesConnected: 1}
	if !p.Same(copy) {
		t.Fatal("equal connection values treated as a change")
	}
	copy.Connections.PhonesConnected = 0
	if p.Same(copy) {
		t.Fatal("phone disconnect not treated as a change")
	}
	copy.Connections = nil
	if p.Same(copy) {
		t.Fatal("legacy unknown treated as explicit report")
	}
}
