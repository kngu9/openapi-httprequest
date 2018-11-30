package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/iancoleman/strcase"
	"github.com/juju/gnuflag"
	"github.com/mhemmings/openapi-httprequest/meta"
	oas "github.com/mhemmings/openapi-httprequest/openapi"
	"github.com/mhemmings/openapi-httprequest/templates"
)

type ref struct {
	Name      string
	SchemaRef *openapi3.SchemaRef
}

var references = make(map[string]ref)

var printCmdUsage = func() {
	fmt.Printf("usage: openapi-httprequest [flags] openapidoc.yaml\n\n")
	gnuflag.PrintDefaults()
}

func main() {
	gnuflag.Usage = func() {
		printCmdUsage()
		os.Exit(2)
	}
	outputDir := gnuflag.String("outputdir", "output/", "The output directory to save generated server package")
	serve := gnuflag.Bool("serve", false, "If set, the generated server will be run after generation")
	port := gnuflag.Int("port", 8080, "Used with '--serve'. The port to run the server on")

	gnuflag.Parse(true)
	if gnuflag.NArg() != 1 || gnuflag.Arg(0) == "help" {
		gnuflag.Usage()
	}
	uri := gnuflag.Arg(0)

	m, err := meta.New(meta.Config{
		SpecFolder: uri,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer m.Close()

	if err := m.GenerateSpec(); err != nil {
		log.Fatal(err)
	}
	return

	swagger, err := oas.Load(uri)
	if err != nil {
		log.Fatal(err)
	}

	var arg templates.TemplateArg

	// Build references of top level schema definitions
	for schemaName, schema := range swagger.Components.Schemas {
		references["#/components/schemas/"+schemaName] = ref{
			Name:      strcase.ToCamel(schemaName),
			SchemaRef: schema,
		}
	}

	// Build schema types
	for schemaName, schema := range swagger.Components.Schemas {
		s := schemaRefParse(schema, strcase.ToCamel(schemaName))
		arg.Types = append(arg.Types, &s)
	}

	// Sort schemas types so they appear in alphabetical order at the top of the file
	sort.Sort(arg.Types)

	// Build all the types for paths
	for path, pathItem := range swagger.Paths {
		for method, op := range pathItem.Operations() {
			req := templates.Definition{
				Name: strcase.ToCamel(op.OperationID + "Request"),
				// Embed the the httprequest.Route type
				Properties: []templates.Definition{{
					Tag:     fmt.Sprintf("`httprequest:\"%s %s\"`", method, oas.PathToString(path)),
					TypeStr: "httprequest.Route",
				}},
			}

			handler := templates.Handler{
				Request: req.Name,
			}

			// Get request params
			for _, param := range op.Parameters {
				def := schemaRefParse(param.Value.Schema, strcase.ToCamel(param.Value.Name))
				p := templates.Definition{
					Name:    def.Name,
					Tag:     fmt.Sprintf("`httprequest:\"%s,%s\"`", param.Value.Name, oas.ParamLocation(param.Value.In)),
					TypeStr: def.TypeStr,
				}

				req.Properties = append(req.Properties, p)
			}

			// Get request body
			if op.RequestBody != nil && op.RequestBody.Value.Content["application/json"] != nil {
				if schema := op.RequestBody.Value.Content["application/json"].Schema; schema != nil {
					def := schemaRefParse(schema, "Body")
					p := templates.Definition{
						Name:    def.Name,
						Tag:     "`httprequest:\",body\"`",
						TypeStr: def.Name,
					}

					req.Properties = append(req.Properties, p)
				}
			}
			arg.Types = append(arg.Types, &req)

			// Count the number of non-default responses. If there is one, the naming is easier.
			var totalResponses int
			for respName, _ := range op.Responses {
				if respName != "default" {
					totalResponses++
				}
			}

			// For each response, build a reponse definition and a http handler.
			for respName, response := range op.Responses {
				handler := handler
				if respName == "default" {
					// Don't build the "default" response as this is usually and error.
					// May not be the correct assumption.
					continue
				}

				var resp templates.Definition
				if body := response.Value.Content.Get("application/json"); body != nil {
					resp = schemaRefParse(body.Schema, "")
				}
				var name string
				if totalResponses == 1 {
					name = op.OperationID
				} else {
					name = op.OperationID + respName
				}

				resp.Name = strcase.ToCamel(name + "Response")
				handler.Name = strcase.ToCamel(name)
				handler.Response = resp.Name

				arg.Types = append(arg.Types, &resp)
				arg.Handlers = append(arg.Handlers, &handler)
			}
		}
	}

	err = templates.WriteAll(*outputDir, arg)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Server package saved in:", *outputDir)

	if *serve {
		fmt.Printf("Running API server on port %d\n", *port)
		cmd := exec.Command("go", "run", "./"+filepath.Join(*outputDir, "..."))
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, fmt.Sprintf("PORT=%d", *port))
		if err := cmd.Run(); err != nil {
			fmt.Println(err)
		}
	}
}

// schemaRefParse takes an openapi SchemeRef doc and creates a type Definition to be used in params.go.
// It attempts ro recursively resolve all references.
func schemaRefParse(oasSchema *openapi3.SchemaRef, name string) templates.Definition {
	if oasSchema.Ref != "" {
		r := references[oasSchema.Ref]
		return schemaRefParse(r.SchemaRef, r.Name)
	}

	schema := templates.Definition{
		Name: name,
	}

	if len(oasSchema.Value.Properties) > 0 {
		for propName, prop := range oasSchema.Value.Properties {
			p := schemaRefParse(prop, strcase.ToCamel(propName))
			p.Tag = fmt.Sprintf("`json:\"%s\"`", propName)
			schema.Properties = append(schema.Properties, p)
		}
	} else if oasSchema.Value.Items != nil {
		t := schemaRefParse(oasSchema.Value.Items, oas.TypeString(oasSchema.Value.Items.Value.Type, oasSchema.Value.Items.Value.Format))
		schema.TypeStr = fmt.Sprintf("[]%s", t.Name)
	} else { //native type
		schema.TypeStr = oas.TypeString(oasSchema.Value.Type, oasSchema.Value.Format)
	}

	return schema
}
