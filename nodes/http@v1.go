package nodes

import (
	_ "embed"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
	"github.com/actionforge/actrun-cli/utils"
)

//go:embed http@v1.yml
var httpDefinition string

type HttpNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

var allowedMethods = map[string]struct{}{
	"OPTIONS": {},
	"GET":     {},
	"HEAD":    {},
	"POST":    {},
	"PUT":     {},
	"DELETE":  {},
	"TRACE":   {},
	"CONNECT": {},
}

func buildRequest(c *core.ExecutionState, method, url string, headers []string, reader io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, err
	}

	for _, header := range headers {
		if header == "" {
			continue
		}

		parts := strings.SplitN(header, ":", 2)
		if len(parts) != 2 {
			return nil, core.CreateErr(c, nil, "invalid header: %s", header)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if key != "" {
			req.Header.Set(key, value)
		}
	}

	req.Header.Set("User-Agent", "actionforge")
	return req, nil
}

func (n *HttpNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	reader, err := core.InputValueById[io.Reader](c, n, ni.Core_http_v1_Input_body)
	if err != nil {
		return err
	}

	defer utils.SafeCloseReaderAndIgnoreError(reader)

	method, err := core.InputValueById[string](c, n, ni.Core_http_v1_Input_method)
	if err != nil {
		return err
	}

	method = strings.ToUpper(method)

	_, ok := allowedMethods[method]
	if !ok {
		return core.CreateErr(c, nil, "invalid method: %s", method)
	}

	url, err := core.InputValueById[string](c, n, ni.Core_http_v1_Input_url)
	if err != nil {
		return err
	}

	headers, err := core.InputValueById[[]string](c, n, ni.Core_http_v1_Input_header)
	if err != nil {
		return err
	}

	req, err := buildRequest(c, method, url, headers, reader)
	if err != nil {
		return err
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, connErr := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}

	// Ensure the input reader is closed in all cases.
	// If closing the reader fails without a prior error,
	// treat it as an error which is part of the connection op.
	err = utils.SafeCloseReader(reader)
	if err != nil && connErr == nil {
		connErr = err
	}

	var dsf core.DataStreamFactory

	var statusCode int
	if resp != nil {
		dsf.Reader = resp.Body
		statusCode = resp.StatusCode
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_http_v1_Output_body, dsf, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_http_v1_Output_status_code, statusCode, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	// According to the Golang docs, a non-2xx status code doesn't necessarily
	// trigger an error, so we set one manually for consistency.
	if connErr != nil {
		connErr = core.CreateErr(c, nil, "connection error: %s", connErr)
	} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		connErr = core.CreateErr(c, nil, "non-2xx status code: %d", resp.StatusCode)
	}

	if connErr != nil {
		err = n.Execute(ni.Core_http_v1_Output_exec_err, c, connErr)
		if err != nil {
			return err
		}
	} else {
		err = n.Execute(ni.Core_http_v1_Output_exec_success, c, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(httpDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &HttpNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
