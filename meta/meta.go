// Package meta generates a meta file to keep track of changes
package meta

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/blang/semver"
)

var fileRegex = regexp.MustCompile(`(?P<Group>[a-zA-z0-9]*)\.(?P<Version>[0-9]*_[0-9]*_[0-9]*)\.y(ml|aml)`)

// Config is used to create a new meta instance
type Config struct {
	SpecFolder string
}

func (c *Config) validate() error {
	if c.SpecFolder == "" {
		return errors.New("spec folder must be specified")
	}
	return nil
}

// Meta contains versioning of the OpenAPI project
type Meta struct {
	Config
	groups map[string][]*Migration
}

// New creates a new meta instance
func New(cfg Config) (*Meta, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	if f, err := os.Stat(cfg.SpecFolder); err != nil {
		return nil, fmt.Errorf("could not stat folder: %s", err.Error())
	} else {
		if !f.Mode().IsDir() {
			return nil, errors.New("path given is not a folder")
		}
	}

	files, err := ioutil.ReadDir(cfg.SpecFolder)
	if err != nil {
		return nil, errors.New("unable to read folder")
	}
	meta := &Meta{
		groups: make(map[string][]*Migration),
	}

	// Iterate through files, ignoring directories and filtering out valid names
	for _, f := range files {
		if !f.IsDir() {

			if match := fileRegex.FindAllStringSubmatch(f.Name(), -1); len(match) > 0 {
				group := match[0][1]

				ver, err := semver.Make(strings.Replace(match[0][2], "_", ".", -1))
				if err != nil {
					return nil, fmt.Errorf("unable to format semver: %s, file: %s", match[0][2], f.Name())
				}

				if _, ok := meta.groups[group]; !ok {
					meta.groups[group] = make([]*Migration, 0)
				}

				path := fmt.Sprintf("%s/%s", cfg.SpecFolder, f.Name())
				file, err := os.Open(path)
				if err != nil {
					return nil, fmt.Errorf("unable to open migration file: %s: %s", path, err.Error())
				}

				m, err := newMigration(&migrationConfig{
					Version: &ver,
					File:    file,
				})
				if err != nil {
					return nil, fmt.Errorf("unable to create migration instance: %s", err.Error())
				}

				meta.groups[group] = append(meta.groups[group], m)
			}
		}
	}

	return meta, nil
}

// GenerateSpec will generate spec files for all groups
func (m *Meta) GenerateSpec() error {
	for _, group := range m.groups {
		// Sort the versions first
		sort.Slice(group, func(i, j int) bool {
			return group[i].Version.LT(*group[j].Version)
		})

		prev := group[0]
		prev.Base()

		for i := 1; i < len(group); i++ {
			temp, err := group[i].MigrateFrom(*prev)
			if err != nil {
				return fmt.Errorf("unable to migrate from %s to %s: %s", prev.Version.String(), group[i].Version.String(), err.Error())
			}
			prev = temp
		}

		buff, _ := prev.swag.MarshalJSON()
		fmt.Println(string(buff))
	}

	return nil
}

// Close will close all open files
func (m *Meta) Close() error {
	for _, group := range m.groups {
		for _, migrations := range group {
			if err := migrations.Close(); err != nil {
				return fmt.Errorf("unable to close migration file %s: %s", migrations.File.Name(), err.Error())
			}
		}
	}
	return nil
}
