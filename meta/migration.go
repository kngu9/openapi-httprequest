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
	*migrationConfig
	buff []byte

	loader *openapi3.SwaggerLoader
	swag   *openapi3.Swagger

	pathVer   map[string]*semver.Version
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

	return &Migration{
		migrationConfig: cfg,
		buff:            buff,
		loader:          openapi3.NewSwaggerLoader(),
		// swag:            swag,
		// schemaVer:       schemaVer,
	}, nil
}

// Base is called to load the initial schema
func (m *Migration) Base() error {
	swag, err := m.loader.LoadSwaggerFromYAMLData(m.buff)
	if err != nil {
		return fmt.Errorf("unable to load file into swagger format: %s", err.Error())
	}
	m.swag = swag

	ver, err := semver.Make(swag.Info.Version)
	if err != nil {
		return fmt.Errorf("invalid semver %s: %s", swag.Info.Version, err.Error())
	}
	if m.Version.Compare(ver) != 0 {
		return fmt.Errorf("version mismatch, version of file: %s, version of spec: %s", m.Version.String(), ver.String())
	}

	schemaVer := make(map[string]*semver.Version)
	for k := range swag.Components.Schemas {
		schemaVer[k] = &ver
	}
	m.schemaVer = schemaVer

	pathVer := make(map[string]*semver.Version)
	for k := range swag.Paths {
		pathVer[k] = &ver
	}
	m.pathVer = pathVer

	return nil
}

// MigrateFrom incrementally migrates the schema
func (m *Migration) MigrateFrom(prev Migration) (*Migration, error) {
	if err := m.loader.ResolveRefsIn(prev.swag); err != nil {
		return nil, fmt.Errorf("unable to resolve refs: %s", err.Error())
	}

	swag, err := m.loader.LoadSwaggerFromYAMLData(m.buff)
	if err != nil {
		return nil, fmt.Errorf("unable to load swagger data: %s", err.Error())
	}

	schemaVer := make(map[string]*semver.Version)
	for k := range swag.Components.Schemas {
		schemaVer[k] = m.Version
	}
	m.schemaVer = schemaVer

	pathVer := make(map[string]*semver.Version)
	for k := range swag.Paths {
		pathVer[k] = m.Version
	}
	m.pathVer = pathVer

	for k, v := range swag.Components.Schemas {
		m.schemaVer[k] = m.Version
		swag.Components.Schemas[k] = v
	}

	for k, v := range swag.Paths {
		m.pathVer[k] = m.Version
		swag.Paths[k] = v
	}

	return &Migration{
		migrationConfig: m.migrationConfig,
		swag:            swag,
		loader:          m.loader,
	}, nil
}

// Close terminates all resources
func (m *Migration) Close() error {
	if err := m.File.Close(); err != nil {
		return fmt.Errorf("unable to close file %s: %s", m.File.Name(), err.Error())
	}
	return nil
}
