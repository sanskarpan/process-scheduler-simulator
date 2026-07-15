package process

import "testing"

// ISSUE-028: VRuntime must advance for processes with Weight > 1024 (nice < 0).
// Before the fix, int64(duration * 1024 / p.Weight) truncated to 0 for Weight=1280
// (nice=-1), causing CFS to monopolize these processes.

func TestVRuntimeAdvancesForHighWeight(t *testing.T) {
	p := NewProcess(1, "P1", 0, 10, 0)
	p.SetNice(-1) // Weight = 1280 > 1024
	p.State = StateReady
	before := p.VRuntime
	p.Execute(0, 1)
	if p.VRuntime == before {
		t.Errorf("VRuntime did not advance for Weight=%d (nice=-1): still %d",
			p.Weight, p.VRuntime)
	}
}

func TestVRuntimeAdvancesForMaxNice(t *testing.T) {
	p := NewProcess(1, "P1", 0, 100, 0)
	p.SetNice(-20)
	p.State = StateReady
	before := p.VRuntime
	p.Execute(0, 1)
	if p.VRuntime == before {
		t.Errorf("VRuntime did not advance for Weight=%d (nice=-20): still %d",
			p.Weight, p.VRuntime)
	}
}

func TestVRuntimeDefaultWeight(t *testing.T) {
	p := NewProcess(1, "P1", 0, 10, 0) // Weight=1024 (nice=0)
	p.State = StateReady
	p.Execute(0, 1)
	// int64(1) * VRuntimeScale / int64(1024)
	want := int64(VRuntimeScale) / 1024
	if p.VRuntime != want {
		t.Errorf("VRuntime = %d, want %d for default Weight=1024", p.VRuntime, want)
	}
}

func TestVRuntimePositiveNice(t *testing.T) {
	p := NewProcess(1, "P1", 0, 10, 0)
	p.SetNice(5) // Weight < 1024; VRuntime should increase faster
	p.State = StateReady
	before := p.VRuntime
	p.Execute(0, 1)
	if p.VRuntime <= before {
		t.Errorf("VRuntime did not advance for Weight=%d (nice=5): still %d",
			p.Weight, p.VRuntime)
	}
}
