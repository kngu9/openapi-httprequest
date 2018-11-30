package meta

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/blang/semver"
)

type migrationConfig struct {
	Version *semver.Version
	File    *os.File
}

func (c *migrationConfig) validate() error {
	if c.Version == nil {
		return errors.New("version must be specified")
	}
	if c.File == nil {
		return errors.New("file handle must be specified")
	}
	return nil
}

// Migration represents a single migration for a group
type Migration struct {
	*migrationConfig `yaml:",omitempty"`

	swag      *openapi3.Swagger
	schemaVer map[string]*semver.Version
}

func newMigration(cfg *migrationConfig) (*Migration, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	buff, err := ioutil.ReadAll(cfg.File)
	if err != nil {
		return nil, fmt.Errorf("unable to read file: %s", err.Error())
	}

	loader := openapi3.NewSwaggerLoader()
	swag, err := loader.LoadSwaggerFromYAMLData(buff)
	if err != nil {
		return nil, fmt.Errorf("unable to load file into swagger format: %s", err.Error())
	}

	ver, err := semver.Make(swag.Info.Version)
	if err != nil {
		return nil, fmt.Errorf("invalid semver %s: %s", swag.Info.Version, err.Error())
	}
	if cfg.Version.Compare(ver) != 0 {
		return nil, fmt.Errorf("version mismatch, version of file: %s, version of spec: %s", cfg.Version.String(), ver.String())
	}

	schemaVer := make(map[string]*semver.Version)
	for k := range swag.Components.Schemas {
		schemaVer[k] = &ver
	}

	return &Migration{
		migrationConfig: cfg,
		swag:            swag,
		schemaVer:       schemaVer,
	}, nil
}

// MigrateFrom incrementally migrates the schema
func (m *Migration) MigrateFrom(prev Migration) (*Migration, error) {
	for k, v := range m.swag.Components.Schemas {
		prev.schemaVer[k] = m.Version
		prev.swag.Components.Schemas[k] = v
	}

	return &prev, nil
}

// Close terminates all resources
func (m *Migration) Close() error {
	if err := m.File.Close(); err != nil {
		return fmt.Errorf("unable to close file %s: %s", m.File.Name(), err.Error())
	}
	return nil
}
