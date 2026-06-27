package store

import (
	"fmt"
	"sync"
	"testing"
)

func makeRecord(id string) Record {
	return Record{ID: id, Algorithm: "fcfs"}
}

func TestSaveAndList(t *testing.T) {
	s := New(10)
	s.Save(makeRecord("r1"))
	s.Save(makeRecord("r2"))
	s.Save(makeRecord("r3"))

	list := s.List()
	if len(list) != 3 {
		t.Fatalf("len = %d, want 3", len(list))
	}
	// Newest first.
	if list[0].ID != "r3" || list[1].ID != "r2" || list[2].ID != "r1" {
		t.Errorf("order = %s, %s, %s; want r3, r2, r1", list[0].ID, list[1].ID, list[2].ID)
	}
}

func TestGet(t *testing.T) {
	s := New(10)
	s.Save(makeRecord("known"))

	rec, ok := s.Get("known")
	if !ok {
		t.Fatal("Get(known) = false, want true")
	}
	if rec.ID != "known" {
		t.Errorf("rec.ID = %q", rec.ID)
	}

	if _, ok := s.Get("missing"); ok {
		t.Fatal("Get(missing) = true, want false")
	}
}

func TestCapacityEviction(t *testing.T) {
	s := New(2)
	s.Save(makeRecord("r1"))
	s.Save(makeRecord("r2"))
	s.Save(makeRecord("r3"))

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2 (capacity)", len(list))
	}
	// r1 should be evicted; r2 and r3 remain, newest first.
	ids := map[string]bool{list[0].ID: true, list[1].ID: true}
	if ids["r1"] {
		t.Errorf("r1 should have been evicted")
	}
	if !ids["r2"] || !ids["r3"] {
		t.Errorf("expected r2 and r3 to remain, got %v", ids)
	}
}

func TestClear(t *testing.T) {
	s := New(10)
	s.Save(makeRecord("r1"))
	s.Save(makeRecord("r2"))
	s.Clear()

	if list := s.List(); len(list) != 0 {
		t.Fatalf("len = %d after Clear, want 0", len(list))
	}
}

func TestConcurrentAccess(t *testing.T) {
	const goroutines = 50
	const capacity = 10
	s := New(capacity)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			s.Save(makeRecord(fmt.Sprintf("g%d", i)))
			_ = s.List()
		}()
	}
	wg.Wait()

	if list := s.List(); len(list) > capacity {
		t.Fatalf("len = %d, want <= capacity %d", len(list), capacity)
	}
}
