package packager

import (
	"crypto/md5"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver"
	"github.com/cloudfoundry/libbuildpack"
	"github.com/pkg/errors"
)

type Dependency struct {
	URI          string   `yaml:"uri"`
	File         string   `yaml:"file"`
	SHA256       string   `yaml:"sha256"`
	Name         string   `yaml:"name"`
	Version      string   `yaml:"version"`
	Stacks       []string `yaml:"cf_stacks"`
	Modules      []string `yaml:"modules"`
	Source       string   `yaml:"source"`
	SourceSHA256 string   `yaml:"source_sha256"`
	CNB          bool     `yaml:"cnb"`
}

func (d Dependency) Download(cacheDir string) (File, error) {
	if d.CNB {
		return d.downloadCNB(cacheDir)
	}

	return d.downloadDependency(cacheDir)
}

func (d Dependency) UpdateDependencyMap(dependencyMap interface{}, file File) error {
	dep, ok := dependencyMap.(map[interface{}]interface{})
	if !ok {
		return fmt.Errorf("Could not cast deps[idx] to map[interface{}]interface{}")
	}

	dep["file"] = file.Name
	if d.CNB {
		sha, err := calcSha(file.Path)
		if err != nil {
			return err
		}

		dep["sha256"] = sha
	}

	return nil
}

func (d Dependency) downloadCNB(cacheDir string) (File, error) {

	outputFileArchive := filepath.Join("dependencies", fmt.Sprintf("%x", md5.Sum([]byte(d.Source))), filepath.Base(d.Source))
	outputFile := strings.TrimSuffix(outputFileArchive, filepath.Ext(outputFileArchive))
	tmpFileDst := filepath.Join("dependencies", fmt.Sprintf("%x", md5.Sum([]byte(d.Source))), "source")
	tmpFile := tmpFileDst + ".tgz"
	fmt.Println("outputFile ", outputFile)
	fmt.Println("tmpFileDst ", tmpFileDst)
	fmt.Println("tmpFile ", tmpFile)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		log.Fatalf("error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(cacheDir, outputFile)); err != nil {
		if err := downloadFromURI(d.Source, filepath.Join(cacheDir, tmpFile)); err != nil {
			return File{}, err
		}

		if err := checkSha256(filepath.Join(cacheDir, tmpFile), d.SourceSHA256); err != nil {
			return File{}, err
		}

		fmt.Println("Extracting file to ", filepath.Join(cacheDir, tmpFileDst))
		if err := libbuildpack.ExtractTarGz(filepath.Join(cacheDir, tmpFile), filepath.Join(cacheDir, tmpFileDst)); err != nil {
			return File{}, errors.Wrap(err, "problem uncompressing CNB")
		}

		fmt.Println("Extracted file !!!! YAY")

		fmt.Println("running install_tools.sh in: ")
		fmt.Println(filepath.Join(cacheDir, tmpFileDst))
		tools_cmd := exec.Command("./scripts/install_tools.sh")
		tools_cmd.Dir = filepath.Join(cacheDir, tmpFileDst)
		tools_cmd.Stdout = os.Stdout
		if err := tools_cmd.Run(); err != nil {
			errString := "problem installing tools"
			if ee, ok := err.(*exec.ExitError); ok {
				errString = string(ee.Stderr)
			}

			return File{}, errors.Wrap(err, errString)
		}

		fmt.Println("Installed Tools")

		cmd := exec.Command("./.bin/packager", "-archive", filepath.Join(cacheDir, outputFile))
		cmd.Dir = filepath.Join(cacheDir, tmpFileDst)
		var output []byte
		if output, err = cmd.CombinedOutput(); err != nil {
			return File{}, errors.Wrap(err, string(output))
		}

		fmt.Println("packager output")
		fmt.Println(string(output))

		fmt.Println("Packaged buildpack")

	}

	return File{outputFileArchive, filepath.Join(cacheDir, outputFileArchive)}, nil
}

func (d Dependency) downloadDependency(cacheDir string) (File, error) {
	file := filepath.Join("dependencies", fmt.Sprintf("%x", md5.Sum([]byte(d.URI))), filepath.Base(d.URI))
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		log.Fatalf("error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(cacheDir, file)); err != nil {
		if err := downloadFromURI(d.URI, filepath.Join(cacheDir, file)); err != nil {
			return File{}, err
		}
	}

	if err := checkSha256(filepath.Join(cacheDir, file), d.SHA256); err != nil {
		return File{}, err
	}

	return File{file, filepath.Join(cacheDir, file)}, nil
}

type Dependencies []Dependency

type Manifest struct {
	Language     string       `yaml:"language"`
	Stack        string       `yaml:"stack"`
	IncludeFiles []string     `yaml:"include_files"`
	PrePackage   string       `yaml:"pre_package"`
	Dependencies Dependencies `yaml:"dependencies"`
	Defaults     []struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"default_versions"`
}

type File struct {
	Name, Path string
}

func (d Dependencies) Len() int      { return len(d) }
func (d Dependencies) Swap(i, j int) { d[i], d[j] = d[j], d[i] }
func (d Dependencies) Less(i, j int) bool {
	if d[i].Name < d[j].Name {
		return true
	} else if d[i].Name == d[j].Name {
		v1, e1 := semver.NewVersion(d[i].Version)
		v2, e2 := semver.NewVersion(d[j].Version)
		if e1 == nil && e2 == nil {
			return v1.LessThan(v2)
		} else {
			return d[i].Version < d[j].Version
		}
	}
	return false
}

func (m *Manifest) hasStack(stack string) bool {
	for _, e := range m.Dependencies {
		for _, s := range e.Stacks {
			if s == stack {
				return true
			}
		}
	}
	return false
}

func (m *Manifest) versionsOfDependencyWithStack(depName, stack string) []string {
	versions := []string{}
	for _, e := range m.Dependencies {
		if e.Name == depName {
			for _, s := range e.Stacks {
				if s == stack {
					versions = append(versions, e.Version)
				}
			}
		}
	}
	return versions
}
