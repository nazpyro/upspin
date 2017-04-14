// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//

// +build !windows

package main

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path"
	rtdebug "runtime/debug"
	"testing"

	"bazil.org/fuse"

	"upspin.io/bind"
	"upspin.io/config"
	"upspin.io/factotum"
	"upspin.io/test/testutil"
	"upspin.io/upspin"

	dirserver "upspin.io/dir/inprocess"
	keyserver "upspin.io/key/inprocess"
	storeserver "upspin.io/store/inprocess"
)

var testConfig struct {
	mountpoint string
	cacheDir   string
	root       string
	user       string
}

const (
	perm = 0777
)

// testSetup creates a temporary user config with inprocess services.
func testSetup(name string) (upspin.Config, error) {
	endpoint := upspin.Endpoint{
		Transport: upspin.InProcess,
		NetAddr:   "", // ignored
	}

	f, err := factotum.NewFromDir(testutil.Repo("key", "testdata", "user1")) // Always use user1's keys.
	if err != nil {
		return nil, err
	}

	cfg := config.New()
	cfg = config.SetUserName(cfg, upspin.UserName(name))
	cfg = config.SetPacking(cfg, upspin.EEPack)
	cfg = config.SetKeyEndpoint(cfg, endpoint)
	cfg = config.SetStoreEndpoint(cfg, endpoint)
	cfg = config.SetDirEndpoint(cfg, endpoint)
	cfg = config.SetFactotum(cfg, f)

	bind.RegisterKeyServer(upspin.InProcess, keyserver.New())
	bind.RegisterStoreServer(upspin.InProcess, storeserver.New())
	bind.RegisterDirServer(upspin.InProcess, dirserver.New(cfg))

	publicKey := upspin.PublicKey(fmt.Sprintf("key for %s", name))
	user := &upspin.User{
		Name:      upspin.UserName(name),
		Dirs:      []upspin.Endpoint{cfg.DirEndpoint()},
		Stores:    []upspin.Endpoint{cfg.StoreEndpoint()},
		PublicKey: publicKey,
	}
	key, err := bind.KeyServer(cfg, cfg.KeyEndpoint())
	if err != nil {
		return nil, err
	}
	err = key.Put(user)
	return cfg, err
}

func mount() error {
	// Create a mountpoint. There are 4 possible mountpoints /tmp/upsinfstest[1-4].
	// This lets us set up some /etc/fstab entries on Linux for the tests and
	// avoid using sudo.
	var err error
	found := false
	for i := 1; i < 5; i++ {
		testConfig.mountpoint = fmt.Sprintf("/tmp/upspinfstest%d", i)
		if err = os.Mkdir(testConfig.mountpoint, 0777); err == nil {
			found = true
			break
		}
	}
	if !found {
		for i := 1; i < 5; i++ {
			// No free mountpoint found. Just pick one and hope we aren't
			// breaking another test.
			testConfig.mountpoint = fmt.Sprintf("/tmp/upspinfstest%d", i)
			fuse.Unmount(testConfig.mountpoint)
			os.RemoveAll(testConfig.mountpoint)
			if err = os.Mkdir(testConfig.mountpoint, 0777); err == nil {
				found = true
				break
			}
		}
	}
	if !found {
		return err
	}

	// Set up a user config.
	testConfig.user = "tester@google.com"
	cfg, err := testSetup(testConfig.user)
	if err != nil {
		return err
	}

	// A directory for cache files.
	testConfig.cacheDir, err = ioutil.TempDir("/tmp", "upspincache")
	if err != nil {
		return err
	}

	// Mount the file system. It will be served in a separate go routine.
	do(cfg, testConfig.mountpoint, testConfig.cacheDir)

	// Create the user root, all tests will need it.
	testConfig.root = path.Join(testConfig.mountpoint, testConfig.user)
	if err := os.Mkdir(testConfig.root, 0777); err != nil {
		return err
	}

	return nil
}

func cleanup() {
	fuse.Unmount(testConfig.mountpoint)
	os.RemoveAll(testConfig.mountpoint)
	os.RemoveAll(testConfig.cacheDir)
}

func TestMain(m *testing.M) {
	if os.Getenv("TRAVIS") == "true" {
		// TravisCI doesn't support FUSE filesystems.
		fmt.Fprintln(os.Stderr, "Skipping upspinfs tests on TravisCI.")
		os.Exit(0)
	}
	if err := mount(); err != nil {
		fmt.Fprintf(os.Stderr, "mount failed: %s", err)
		cleanup()
		os.Exit(1)
	}
	rv := m.Run()
	cleanup()
	os.Exit(rv)
}

func mkTestDir(t *testing.T, name string) string {
	testDir := path.Join(testConfig.root, name)
	if err := os.Mkdir(testDir, perm); err != nil {
		fatal(t, err)
	}
	return testDir
}

func randomBytes(t *testing.T, len int) []byte {
	buf := make([]byte, len)
	if _, err := rand.Read(buf); err != nil {
		fatal(t, err)
	}
	return buf
}

func writeFile(t *testing.T, fn string, buf []byte) *os.File {
	f, err := os.OpenFile(fn, os.O_CREATE|os.O_WRONLY, perm)
	if err != nil {
		fatal(t, err)
	}
	n, err := f.Write(buf)
	if err != nil {
		f.Close()
		fatal(t, err)
	}
	if n != len(buf) {
		f.Close()
		fatalf(t, "%s: wrote %d bytes, expected %d", fn, n, len(buf))
	}
	return f
}

func readAndCheckContents(t *testing.T, fn string, buf []byte) {
	f, err := os.Open(fn)
	if err != nil {
		fatal(t, err)
	}
	defer f.Close()
	rbuf := make([]byte, len(buf))
	n, err := f.Read(rbuf)
	if err != nil {
		fatal(t, err)
	}
	if n != len(buf) {
		fatalf(t, "%s: read %d bytes, expected %d", fn, n, len(buf))
	}
	for i := range buf {
		if buf[i] != rbuf[i] {
			fatalf(t, "%s: error at byte %d", fn, i)
		}
	}
}

func mkFile(t *testing.T, fn string, buf []byte) {
	f := writeFile(t, fn, buf)
	if err := f.Close(); err != nil {
		fatal(t, err)
	}
}

func mkDir(t *testing.T, fn string) {
	if err := os.Mkdir(fn, perm); err != nil {
		fatal(t, err)
	}
}

func remove(t *testing.T, fn string) {
	if err := os.Remove(fn); err != nil {
		fatal(t, err)
	}
	notExist(t, fn, "removal")
}

func notExist(t *testing.T, fn, event string) {
	if _, err := os.Stat(fn); err == nil {
		fatalf(t, "%s: should not exist after %s", fn, event)
	}
}

// TestFile tests creating, writing, reading, and removing a file.
func TestFile(t *testing.T) {
	testDir := mkTestDir(t, "testfile")
	buf := randomBytes(t, 16*1024)

	// Create and write a file.
	fn := path.Join(testDir, "file")
	wf := writeFile(t, fn, buf)

	// Read before close.
	readAndCheckContents(t, fn, buf)

	// Read after close.
	if err := wf.Close(); err != nil {
		t.Fatal(err)
	}
	readAndCheckContents(t, fn, buf)

	// Test Rewriting part of the file.
	for i := 0; i < len(buf)/2; i++ {
		buf[i] = buf[i] ^ 0xff
	}
	wf = writeFile(t, fn, buf[:len(buf)/2])
	if err := wf.Close(); err != nil {
		t.Fatal(err)
	}
	readAndCheckContents(t, fn, buf)
	remove(t, fn)

	if err := os.RemoveAll(testDir); err != nil {
		t.Fatal(err)
	}
}

// TestSymlink tests creating, traversing, reading, and removing symnlinks.
func TestSymlink(t *testing.T) {
	testDir := mkTestDir(t, "testsymlinks")

	// The test will have the following directory structure:
	// dir/
	//   real1 - a real file
	//   sidelink - a link to dir/real
	//   downlink - a symlink to subdir/real
	//   subdir/
	//     real2 - a real file
	//     uplink - a link to dir/real
	//
	dir := path.Join(testDir, "dir")
	mkDir(t, dir)
	real1 := path.Join(dir, "real1")
	mkFile(t, real1, []byte(real1))
	subdir := path.Join(dir, "subdir")
	mkDir(t, subdir)
	real2 := path.Join(subdir, "real2")
	mkFile(t, real2, []byte(real2))

	// Test each link.
	testSymlink(t, path.Join(dir, "sidelink"), real1, "real1", []byte(real1))
	testSymlink(t, path.Join(dir, "downlink"), real2, "subdir/real2", []byte(real2))
	testSymlink(t, path.Join(subdir, "uplink"), real1, "../real1", []byte(real1))

	if err := os.RemoveAll(testDir); err != nil {
		t.Fatal(err)
	}
}

// testSymlink creates and tests a symlink using both rooted and relative names.
func testSymlink(t *testing.T, link, rooted, relative string, contents []byte) {
	// Create and test using rooted name.
	if err := os.Symlink(rooted, link); err != nil {
		fatal(t, err)
	}
	val, err := os.Readlink(link)
	if err != nil {
		fatal(t, err)
	}
	if val != relative {
		fatalf(t, "%s: Readlink returned %s, expected %s:]", link, val, relative)
	}
	remove(t, link)

	// Create and test using relative name.
	if err := os.Symlink(relative, link); err != nil {
		fatal(t, err)
	}
	val, err = os.Readlink(link)
	if err != nil {
		fatal(t, err)
	}
	if val != relative {
		fatalf(t, "%s: Readlink returned %s, expected %s", link, val, relative)
	}
}

// TestRename tests renaming a file.
func TestRename(t *testing.T) {
	testDir := mkTestDir(t, "testrename")

	// Check that file is renamed and old name is no longer valid.
	original := path.Join(testDir, "original")
	newname := path.Join(testDir, "newname")
	mkFile(t, original, []byte(original))
	if err := os.Rename(original, newname); err != nil {
		t.Fatal(err)
	}
	readAndCheckContents(t, newname, []byte(original))
	notExist(t, original, "rename")
	remove(t, newname)

	// Test on more time but with "newname" preexisting. It should be replaced.
	mkFile(t, original, []byte(original))
	mkFile(t, newname, []byte(newname))
	if err := os.Rename(original, newname); err != nil {
		t.Fatal(err)
	}
	readAndCheckContents(t, newname, []byte(original))
	notExist(t, original, "rename")

	if err := os.RemoveAll(testDir); err != nil {
		t.Fatal(err)
	}
}

// TestAccess tests access control. This is not a rigorous right test, we just want
// to ensure that the access file is checked at file creation and open.
func TestAccess(t *testing.T) {
	testDir := mkTestDir(t, "testaccess")

	// First check that we can create a file.
	fn := path.Join(testDir, "newname")
	mkFile(t, fn, []byte(fn))

	// Now create an access fn allowing only read and list.
	access := path.Join(testDir, "Access")
	mkFile(t, access, []byte("r,l: "+testConfig.user+"\n"))

	// We should still be able to read.
	readAndCheckContents(t, fn, []byte(fn))

	// Rewrite should fail.
	if _, err := os.OpenFile(fn, os.O_WRONLY, perm); err == nil {
		t.Fatalf("%s: can write after read only access", fn)
	}

	// Append should fail.
	if _, err := os.OpenFile(fn, os.O_WRONLY|os.O_APPEND, perm); err == nil {
		t.Fatalf("%s: can write after read only access", fn)
	}

	// Creating new files should fail.
	fn = fn + ".new"
	if _, err := os.OpenFile(fn, os.O_WRONLY|os.O_CREATE, perm); err == nil {
		t.Fatalf("%s: can write after read only access", fn)
	}

	// Removing Access should work.
	remove(t, access)

	if err := os.RemoveAll(testDir); err != nil {
		t.Fatal(err)
	}
}

func fatal(t *testing.T, args ...interface{}) {
	t.Log(fmt.Sprintln(args...))
	t.Log(string(rtdebug.Stack()))
	t.FailNow()
}

func fatalf(t *testing.T, format string, args ...interface{}) {
	t.Log(fmt.Sprintf(format, args...))
	t.Log(string(rtdebug.Stack()))
	t.FailNow()
}
