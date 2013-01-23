package main

import (
	"io/ioutil"
	"os"
	"testing"
)

type fixturePair struct {
	json     []byte
	triplets []triplet
}

func (f *fixturePair) check(t *testing.T, sdb *serveDb) {
	for _, triplet := range f.triplets {
		resolvTok, ok := sdb.Resolve(triplet.I)
		if !ok {
			t.Fatalf("Expected to find identifier %q", triplet.I)
		}

		if triplet.T != resolvTok {
			t.Fatalf("Expected to resolve to %v, "+
				"but got %v instead", triplet.T, resolvTok)
		}
	}

}

var fixtures = []fixturePair{
	{
		json: []byte(`{"serves": ` +
			`[{"i": "apple", "t": "chocolate", ` +
			`"p": "/p1/log.sock"}, ` +
			`{"i": "banana", "t": "vanilla", ` +
			`"p": "/p2/log.sock"}]}`),
		triplets: []triplet{
			{I: "apple", T: "chocolate"},
			{I: "banana", T: "vanilla"},
		},
	},
	{
		json: []byte(`{"serves": ` +
			`[{"i": "bed", "t": "pillow", ` +
			`"p": "/p1/log.sock"}, ` +
			`{"i": "nightstand", "t": "alarm clock", ` +
			`"p": "/p2/log.sock"}]}`),
		triplets: []triplet{
			{I: "apple", T: "chocolate"},
			{I: "banana", T: "vanilla"},
		},
	},
}

func newTmpDb(t *testing.T) string {
	name, err := ioutil.TempDir("", "test_")
	if err != nil {
		t.Fatalf("Could not create temporary directory for test: %v",
			err)
	}

	return name
}

func TestEmptyDB(t *testing.T) {
	name := newTmpDb(t)
	defer os.RemoveAll(name)

	sdb := newServeDb(name)
	if err := sdb.Poll(); err != nil {
		t.Fatalf("Poll on an empty directory should succeed, "+
			"instead failed: %v", err)
	}
}

func TestMultipleLoad(t *testing.T) {
	name := newTmpDb(t)
	defer os.RemoveAll(name)

	sdb := newServeDb(name)
	for i := range fixtures {
		fixture := &fixtures[i]
		ioutil.WriteFile(sdb.newPath(), fixture.json, 0400)

		if err := sdb.Poll(); err != nil {
			t.Fatalf("Poll should succeed with valid input, "+
				"instead: %v", err)
		}

		_, err := os.Stat(sdb.loadedPath())
		if err != nil {
			t.Fatalf("Input should be successfully loaded to %v, "+
				"but the file could not be stat()ed for some "+
				"reason: %v", sdb.loadedPath(), err)
		}

		fixture.check(t, sdb)
	}
}

func TestIntermixedGoodBadInput(t *testing.T) {
	name := newTmpDb(t)
	defer os.RemoveAll(name)

	sdb := newServeDb(name)

	// Write out some valid input to serves.new.
	writeLoadFixture := func(fixture *fixturePair) {
		ioutil.WriteFile(sdb.newPath(), fixture.json, 0400)
		if err := sdb.Poll(); err != nil {
			t.Fatalf("Poll should succeed with valid input, "+
				"instead: %v", err)
		}
	}

	fixture := &fixtures[0]
	writeLoadFixture(fixture)

	// Write a bad serves.new file.
	ioutil.WriteFile(sdb.newPath(), []byte(`{}`), 0400)
	if err := sdb.Poll(); err != nil {
		t.Fatalf("Poll should succeed with invalid input, "+
			"instead: %v", err)
	}

	// Confirm that the original, good fixture's data is still in
	// place.
	fixture.check(t, sdb)

	// Confirm that the serves.rej and last_error file have been
	// made.
	_, err := os.Stat(sdb.errPath())
	if err != nil {
		t.Fatalf("last_error file should exist: %v", err)
	}

	_, err = os.Stat(sdb.rejPath())
	if err != nil {
		t.Fatalf("serves.rej should exist: %v", err)
	}

	// Submit a new set of good input, to see if the last_error
	// and serves.rej are unlinked.
	secondFixture := &fixtures[1]
	writeLoadFixture(secondFixture)

	// Make sure new data was loaded properly.
	secondFixture.check(t, sdb)

	// Check that the old reject file and error file are gone.
	_, err = os.Stat(sdb.errPath())
	if err == nil || !os.IsNotExist(err) {
		t.Fatalf("last_error file shouldn't exist: %v", err)
	}

	_, err = os.Stat(sdb.rejPath())
	if err == nil || !os.IsNotExist(err) {
		t.Fatalf("serves.rej shouldn't exist: %v", err)
	}
}

func TestFirstTimeLoadPoll(t *testing.T) {
	name := newTmpDb(t)
	defer os.RemoveAll(name)

	sdb := newServeDb(name)

	// Write directly to the serves.loaded file, which is not the
	// normal way thing are done; Poll() should move things around
	// outside a test environment.
	fixture := &fixtures[0]
	ioutil.WriteFile(sdb.loadedPath(), fixture.json, 0400)

	if err := sdb.Poll(); err != nil {
		t.Fatalf("Poll should succeed with valid input, "+
			"instead: %v", err)
	}

	fixture.check(t, sdb)
}

func TestEmptyPoll(t *testing.T) {
	name := newTmpDb(t)
	defer os.RemoveAll(name)

	sdb := newServeDb(name)
	err := sdb.Poll()
	if err != nil {
		t.Fatalf("An empty database should not cause an error, "+
			"but got: %v", err)
	}

	if sdb.identToServe == nil {
		t.Fatal("An empty database should yield an " +
			"empty routing table.")
	}
}

func TestFirstLoadBad(t *testing.T) {
	name := newTmpDb(t)
	defer os.RemoveAll(name)

	sdb := newServeDb(name)

	// Write a bad serves.new file.
	ioutil.WriteFile(sdb.newPath(), []byte(`{}`), 0400)
	if err := sdb.Poll(); err != nil {
		t.Fatalf("Poll should succeed with invalid input, "+
			"instead: %v", err)
	}

	err := sdb.Poll()
	if err != nil {
		t.Fatalf("Rejected input should not cause an error, "+
			"but got: %v", err)
	}

	// Confirm that the serves.rej and last_error file have been
	// made.
	_, err = os.Stat(sdb.errPath())
	if err != nil {
		t.Fatalf("last_error file should exist: %v", err)
	}

	_, err = os.Stat(sdb.rejPath())
	if err != nil {
		t.Fatalf("serves.rej should exist: %v", err)
	}
}
