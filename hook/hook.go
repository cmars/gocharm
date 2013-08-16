// The hook package provides a Go interface to the
// charm hook commands.
package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"launchpad.net/juju-core/worker/uniter/jujuc"
	"log"
	"strings"
)

func (ctxt *Context) IsRelationHook() bool {
	return ctxt.RelationName != ""
}

func (ctxt *Context) OpenPort(proto string, port int) error {
	_, err := ctxt.run("open-port", fmt.Sprintf("%d/%s", port, proto))
	return err
}

func (ctxt *Context) ClosePort(proto string, port int) error {
	_, err := ctxt.run("close-port", fmt.Sprintf("%d/%s", port, proto))
	return err
}

// PrivateAddress returns the public address of the local unit.
func (ctxt *Context) PublicAddress() (string, error) {
	out, err := ctxt.run("unit-get", "public-address")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// PrivateAddress returns the private address of the local unit.
func (ctxt *Context) PrivateAddress() (string, error) {
	out, err := ctxt.run("unit-get", "private-address")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Log logs a message through the juju logging facility.
func (ctxt *Context) Logf(f string, a ...interface{}) error {
	_, err := ctxt.run("juju-log", fmt.Sprintf(f, a...))
	return err
}

// GetRelation returns the value with the given key from the
// relation and unit that triggered the hook execution.
// It is equivalent to GetRelationUnit(key, RelationId, RemoteUnit).
func (ctxt *Context) GetRelation(key string) (string, error) {
	return ctxt.GetRelationUnit(key, ctxt.RelationId, ctxt.RemoteUnit)
}

// GetRelationUnit returns the value with the given key
// from the given unit associated with the relation with the
// given id.
func (ctxt *Context) GetRelationUnit(key string, relationId, unit string) (string, error) {
	var val string
	if err := ctxt.runJson(&val, "relation-get", "--format", "json", "-r", relationId, "--", key, unit); err != nil {
		return "", err
	}
	return val, nil
}

// GetAllRelation returns all the settings for the relation
// and unit that triggered the hook execution.
// It is equivalent to GetAllRelationUnit(RelationId, RemoteUnit).
func (ctxt *Context) GetAllRelation() (map[string]string, error) {
	return ctxt.GetAllRelationUnit(ctxt.RelationId, ctxt.RemoteUnit)
}

// GetAllRelationUnit returns all the settings from the given unit associated
// with the relation with the given id.
func (ctxt *Context) GetAllRelationUnit(relationId, unit string) (map[string]string, error) {
	var val map[string]string
	if err := ctxt.runJson(&val, "relation-get", "-r", relationId, "--format", "json", "--", "-", unit); err != nil {
		return nil, err
	}
	return val, nil
}

// RelationIds returns all the relation ids associated
// with the relation with the given name,
func (ctxt *Context) RelationIds(relationName string) ([]string, error) {
	var val []string
	if err := ctxt.runJson(&val, "relation-ids", "--format", "json", "--", relationName); err != nil {
		return nil, err
	}
	return val, nil
}

func (ctxt *Context) RelationUnits(relationId string) ([]string, error) {
	var val []string
	if err := ctxt.runJson(&val, "relation-list", "--format", "json", "--", relationId); err != nil {
		return nil, err
	}
	return val, nil
}

// AllRelationUnits returns a map from all the relation ids
// for the relation with the given name to all the
// units with that name
func (ctxt *Context) AllRelationUnits(relationName string) (map[string][]string, error) {
	allUnits := make(map[string][]string)
	ids, err := ctxt.RelationIds(relationName)
	if err != nil {
		return nil, fmt.Errorf("cannot get relation ids: %v", err)
	}
	for _, id := range ids {
		units, err := ctxt.RelationUnits(id)
		if err != nil {
			return nil, fmt.Errorf("cannot get relation units for id %q: %v", id, err)
		}
		allUnits[id] = units
	}
	return allUnits, nil
}

// SetRelation sets the given key-value pairs on the current relation instance.
func (ctxt *Context) SetRelation(keyvals ...string) error {
	return ctxt.SetRelationWithId(ctxt.RelationId, keyvals...)
}

// SetRelationWithId sets the given key-value pairs
// on the relation with the given id.
func (ctxt *Context) SetRelationWithId(relationId string, keyvals ...string) error {
	if len(keyvals)%2 != 0 {
		return fmt.Errorf("invalid key/value count")
	}
	if len(keyvals) == 0 {
		return nil
	}
	args := make([]string, 0, 3+len(keyvals)/2)
	args = append(args, "-r", relationId, "--")
	for i := 0; i < len(keyvals); i += 2 {
		args = append(args, fmt.Sprintf("%s=%s", keyvals[i], keyvals[i+1]))
	}
	_, err := ctxt.run("relation-set", args...)
	return err
}

func (ctxt *Context) GetConfig(key string) (interface{}, error) {
	var val interface{}
	if err := ctxt.runJson(&val, "config-get", "--format", "json", "--", key); err != nil {
		return nil, err
	}
	return val, nil
}

func (ctxt *Context) GetAllConfig() (map[string]interface{}, error) {
	var val map[string]interface{}
	if err := ctxt.runJson(&val, "config-get", "--format", "json"); err != nil {
		return nil, err
	}
	return val, nil
}

func (ctxt *Context) run(cmd string, args ...string) (stdout []byte, err error) {
	req := jujuc.Request{
		ContextId: ctxt.jujucContextId,
		// We will never use a command that uses a path name,
		// but jujuc checks for an absolute path.
		Dir:         "/",
		CommandName: cmd,
		Args:        args,
	}
	log.Printf("run req %#v", req)
	var resp jujuc.Response
	err = ctxt.jujucClient.Call("Jujuc.Main", req, &resp)
	if err != nil {
		return nil, fmt.Errorf("cannot call jujuc.Main: %v", err)
	}
	if resp.Code == 0 {
		return resp.Stdout, nil
	}
	errText := strings.TrimSpace(string(resp.Stderr))
	if strings.HasPrefix(errText, "error: ") {
		errText = errText[len("error: "):]
	}
	return nil, errors.New(errText)
}

func (ctxt *Context) runJson(dst interface{}, cmd string, args ...string) error {
	out, err := ctxt.run(cmd, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, dst); err != nil {
		return fmt.Errorf("cannot parse command output %q into %T: %v", out, dst, err)
	}
	return nil
}