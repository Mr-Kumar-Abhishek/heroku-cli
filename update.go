package main

import (
	"compress/gzip"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/nightlyone/lockfile"
)

var updateTopic = &Topic{
	Name:        "update",
	Description: "update heroku-cli",
}

var updateCmd = &Command{
	Topic:       "update",
	Description: "updates heroku-cli",
	Args:        []Arg{{Name: "channel", Optional: true}},
	Run: func(ctx *Context) {
		channel := ctx.Args["channel"]
		if channel == "" {
			channel = "master"
		}
		Errf("updating plugins... ")
		if err := node.UpdatePackages(); err != nil {
			panic(err)
		}
		Errln("done")
		manifest := getUpdateManifest(channel)
		build := manifest.Builds[runtime.GOOS][runtime.GOARCH]
		Errf("updating to %s (%s)... ", manifest.Version, manifest.Channel)
		update(build.URL, build.Sha1)
		Errln("done")
	},
}

var binPath = filepath.Join(AppDir, "heroku-cli")
var updateLockPath = filepath.Join(AppDir, "updating.lock")

func init() {
	if runtime.GOOS == "windows" {
		binPath = binPath + ".exe"
	}
}

// UpdateIfNeeded checks for and performs an autoupdate if there is a new version out.
func UpdateIfNeeded() {
	if !updateNeeded() {
		return
	}
	node.UpdatePackages()
	manifest := getUpdateManifest(Channel)
	if manifest.Version == Version {
		// Set timestamp of bin so we don't update again
		os.Chtimes(binPath, time.Now(), time.Now())
		return
	}
	if !updatable() {
		Errf("Out of date: You are running %s but %s is out.\n", Version, manifest.Version)
		return
	}
	// Leave out updating text until heroku-cli is used in place of ruby cli
	// So it doesn't confuse users with 2 different version numbers
	//Errf("Updating to %s... ", manifest.Version)
	build := manifest.Builds[runtime.GOOS][runtime.GOARCH]
	update(build.URL, build.Sha1)
	//Errln("done")
	execBin()
	os.Exit(0)
}

func updateNeeded() bool {
	if Version == "dev" {
		return false
	}
	f, err := os.Stat(binPath)
	if err != nil {
		Errln("WARNING: cannot autoupdate. Try running `heroku update` to manually trigger an update.")
		return false
	}
	return f.ModTime().Add(60 * time.Minute).Before(time.Now())
}

type manifest struct {
	Channel, Version string
	Builds           map[string]map[string]struct {
		URL, Sha1 string
	}
}

func getUpdateManifest(channel string) manifest {
	res, err := http.Get("http://d1gvo455cekpjp.cloudfront.net/" + channel + "/manifest.json")
	if err != nil {
		panic(err)
	}
	var m manifest
	json.NewDecoder(res.Body).Decode(&m)
	return m
}

func updatable() bool {
	path, err := filepath.Abs(os.Args[0])
	if err != nil {
		Errln(err)
	}
	return path == binPath
}

func update(url, sha1 string) {
	lock, err := lockfile.New(updateLockPath)
	if err != nil {
		Errln("Cannot initialize update lockfile.")
		panic(err)
	}
	err = lock.TryLock()
	if err != nil {
		panic(err)
	}
	defer lock.Unlock()
	tmp, err := downloadBin(url)
	if err != nil {
		panic(err)
	}
	if fileSha1(tmp) != sha1 {
		panic("SHA mismatch")
	}
	os.Remove(binPath) // on windows you can't rename on top of an existing file
	if err := os.Rename(tmp, binPath); err != nil {
		panic(err)
	}
}

func downloadBin(url string) (string, error) {
	out, err := os.OpenFile(binPath+"~", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return "", err
	}
	defer out.Close()
	client := &http.Client{}
	req, err := http.NewRequest("GET", url+".gz", nil)
	if err != nil {
		return "", err
	}
	req.Header.Add("Accept-Encoding", "gzip")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	uncompressed, err := gzip.NewReader(resp.Body)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(out, uncompressed)
	return out.Name(), err
}

func fileSha1(path string) string {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", sha1.Sum(data))
}

func execBin() {
	cmd := exec.Command(binPath, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}
