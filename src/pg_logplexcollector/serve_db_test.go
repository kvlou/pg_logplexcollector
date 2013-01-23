package main

import (
	"io/ioutil"
	"os"
	"testing"
)

type fixturePair struct {
	json     []byte
	mappings map[string]string
}

func (f *fixturePair) check(t *testing.T, tdb *serveDb) {
	for ident, tok := range f.mappings {
		resolvTok, ok := tdb.Resolve(ident)
		if !ok {
			t.Fatalf("Expected to find identifier %q", ident)
		}

		if tok != resolvTok {
			t.Fatalf("Expected to resolve to %v, "+
				"but got %v instead", tok, resolvTok)
		}
	}

}

var fixtures = []fixturePair{
	{
		json: []byte(`{"serves": ` +
			`{"apple": "orange", "chocolate": "vanilla"}}`),
		mappings: map[string]string{
			"apple":     "orange",
			"chocolate": "vanilla",
		},
	},
	{
		json: []byte(`{"serves": ` +
			`{"bed": "pillow", "lamp": "lightbulb"}}`),
		mappings: map[string]string{
			"bed":  "pillow",
			"lamp": "lightbulb",
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

	tdb := newServeDb(name)
	if err := tdb.Poll(); err != nil {
		t.Fatalf("Poll on an empty directory should succeed, "+
			"instead failed: %v", err)
	}
}

func TestMultipleLoad(t *testing.T) {
	name := newTmpDb(t)
	defer os.RemoveAll(name)

	tdb := newServeDb(name)
	for i := range fixtures {
		fixture := &fixtures[i]
		ioutil.WriteFile(tdb.newPath(), fixture.json, 0400)

		if err := tdb.Poll(); err != nil {
			t.Fatalf("Poll should succeed with valid input, "+
				"instead: %v", err)
		}

		_, err := os.Stat(tdb.loadedPath())
		if err != nil {
			t.Fatalf("Input should be successfully loaded to %v, "+
				"but the file could not be stat()ed for some "+
				"reason: %v", tdb.loadedPath(), err)
		}

		fixture.check(t, tdb)
	}
}

func TestIntermixedGoodBadInput(t *testing.T) {
	name := newTmpDb(t)
	defer os.RemoveAll(name)

	tdb := newServeDb(name)

	// Write out some valid input to serves.new.
	writeLoadFixture := func(fixture *fixturePair) {
		ioutil.WriteFile(tdb.newPath(), fixture.json, 0400)
		if err := tdb.Poll(); err != nil {
			t.Fatalf("Poll should succeed with valid input, "+
				"instead: %v", err)
		}
	}

	fixture := &fixtures[0]
	writeLoadFixture(fixture)

	// Write a bad serves.new file.
	ioutil.WriteFile(tdb.newPath(), []byte(`{}`), 0400)
	if err := tdb.Poll(); err != nil {
		t.Fatalf("Poll should succeed with invalid input, "+
			"instead: %v", err)
	}

	// Confirm that the original, good fixture's data is still in
	// place.
	fixture.check(t, tdb)

	// Confirm that the serves.rej and last_error file have been
	// made.
	_, err := os.Stat(tdb.errPath())
	if err != nil {
		t.Fatalf("last_error file should exist: %v", err)
	}

	_, err = os.Stat(tdb.rejPath())
	if err != nil {
		t.Fatalf("serves.rej should exist: %v", err)
	}

	// Submit a new set of good input, to see if the last_error
	// and serves.rej are unlinked.
	secondFixture := &fixtures[1]
	writeLoadFixture(secondFixture)

	// Make sure new data was loaded properly.
	secondFixture.check(t, tdb)

	// Check that the old reject file and error file are gone.
	_, err = os.Stat(tdb.errPath())
	if err == nil || !os.IsNotExist(err) {
		t.Fatalf("last_error file shouldn't exist: %v", err)
	}

	_, err = os.Stat(tdb.rejPath())
	if err == nil || !os.IsNotExist(err) {
		t.Fatalf("serves.rej shouldn't exist: %v", err)
	}
}

func TestFirstTimeLoadPoll(t *testing.T) {
	name := newTmpDb(t)
	defer os.RemoveAll(name)

	tdb := newServeDb(name)

	// Write directly to the serves.loaded file, which is not the
	// normal way thing are done; Poll() should move things around
	// outside a test environment.
	fixture := &fixtures[0]
	ioutil.WriteFile(tdb.loadedPath(), fixture.json, 0400)

	if err := tdb.Poll(); err != nil {
		t.Fatalf("Poll should succeed with valid input, "+
			"instead: %v", err)
	}

	fixture.check(t, tdb)
}

func TestEmptyPoll(t *testing.T) {
	name := newTmpDb(t)
	defer os.RemoveAll(name)

	tdb := newServeDb(name)
	err := tdb.Poll()
	if err != nil {
		t.Fatalf("An empty database should not cause an error, "+
			"but got: %v", err)
	}

	if tdb.identToServe == nil {
		t.Fatal("An empty database should yield an " +
			"empty routing table.")
	}
}

func TestFirstLoadBad(t *testing.T) {
	name := newTmpDb(t)
	defer os.RemoveAll(name)

	tdb := newServeDb(name)

	// Write a bad serves.new file.
	ioutil.WriteFile(tdb.newPath(), []byte(`{}`), 0400)
	if err := tdb.Poll(); err != nil {
		t.Fatalf("Poll should succeed with invalid input, "+
			"instead: %v", err)
	}

	err := tdb.Poll()
	if err != nil {
		t.Fatalf("Rejected input should not cause an error, "+
			"but got: %v", err)
	}

	// Confirm that the serves.rej and last_error file have been
	// made.
	_, err = os.Stat(tdb.errPath())
	if err != nil {
		t.Fatalf("last_error file should exist: %v", err)
	}

	_, err = os.Stat(tdb.rejPath())
	if err != nil {
		t.Fatalf("serves.rej should exist: %v", err)
	}
}
