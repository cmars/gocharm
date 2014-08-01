// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package main

import (
	"fmt"
	"strings"

	gc "launchpad.net/gocheck"

	"launchpad.net/juju-core/juju/osenv"
	jujutesting "launchpad.net/juju-core/juju/testing"
	keymanagerserver "launchpad.net/juju-core/state/apiserver/keymanager"
	keymanagertesting "launchpad.net/juju-core/state/apiserver/keymanager/testing"
	coretesting "launchpad.net/juju-core/testing"
	"launchpad.net/juju-core/testing/testbase"
	sshtesting "launchpad.net/juju-core/utils/ssh/testing"
)

type AuthorisedKeysSuite struct {
	testbase.LoggingSuite
	jujuHome *coretesting.FakeHome
}

var _ = gc.Suite(&AuthorisedKeysSuite{})

var authKeysCommandNames = []string{
	"add",
	"delete",
	"help",
	"import",
	"list",
}

func (s *AuthorisedKeysSuite) SetUpTest(c *gc.C) {
	s.LoggingSuite.SetUpTest(c)
	s.jujuHome = coretesting.MakeEmptyFakeHome(c)
}

func (s *AuthorisedKeysSuite) TearDownTest(c *gc.C) {
	s.jujuHome.Restore()
	s.LoggingSuite.TearDownTest(c)
}

func (s *AuthorisedKeysSuite) TestHelpCommands(c *gc.C) {
	// Check that we have correctly registered all the sub commands
	// by checking the help output.
	out := badrun(c, 0, "authorised-keys", "--help")
	lines := strings.Split(out, "\n")
	var names []string
	subcommandsFound := false
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) == 1 && f[0] == "commands:" {
			subcommandsFound = true
			continue
		}
		if !subcommandsFound || len(f) == 0 || !strings.HasPrefix(line, "    ") {
			continue
		}
		names = append(names, f[0])
	}
	// The names should be output in alphabetical order, so don't sort.
	c.Assert(names, gc.DeepEquals, authKeysCommandNames)
}

func (s *AuthorisedKeysSuite) assertHelpOutput(c *gc.C, cmd, args string) {
	if args != "" {
		args = " " + args
	}
	expected := fmt.Sprintf("usage: juju authorised-keys %s [options]%s", cmd, args)
	out := badrun(c, 0, "authorised-keys", cmd, "--help")
	lines := strings.Split(out, "\n")
	c.Assert(lines[0], gc.Equals, expected)
}

func (s *AuthorisedKeysSuite) TestHelpList(c *gc.C) {
	s.assertHelpOutput(c, "list", "")
}

func (s *AuthorisedKeysSuite) TestHelpAdd(c *gc.C) {
	s.assertHelpOutput(c, "add", "<ssh key> [...]")
}

func (s *AuthorisedKeysSuite) TestHelpDelete(c *gc.C) {
	s.assertHelpOutput(c, "delete", "<ssh key id> [...]")
}

func (s *AuthorisedKeysSuite) TestHelpImport(c *gc.C) {
	s.assertHelpOutput(c, "import", "<ssh key id> [...]")
}

type keySuiteBase struct {
	jujutesting.JujuConnSuite
}

func (s *keySuiteBase) SetUpSuite(c *gc.C) {
	s.JujuConnSuite.SetUpSuite(c)
	s.PatchEnvironment(osenv.JujuEnvEnvKey, "dummyenv")
}

func (s *keySuiteBase) setAuthorisedKeys(c *gc.C, keys ...string) {
	keyString := strings.Join(keys, "\n")
	err := s.State.UpdateEnvironConfig(map[string]interface{}{"authorized-keys": keyString}, nil, nil)
	c.Assert(err, gc.IsNil)
	envConfig, err := s.State.EnvironConfig()
	c.Assert(err, gc.IsNil)
	c.Assert(envConfig.AuthorizedKeys(), gc.Equals, keyString)
}

func (s *keySuiteBase) assertEnvironKeys(c *gc.C, expected ...string) {
	envConfig, err := s.State.EnvironConfig()
	c.Assert(err, gc.IsNil)
	keys := envConfig.AuthorizedKeys()
	c.Assert(keys, gc.Equals, strings.Join(expected, "\n"))
}

type ListKeysSuite struct {
	keySuiteBase
}

var _ = gc.Suite(&ListKeysSuite{})

func (s *ListKeysSuite) TestListKeys(c *gc.C) {
	key1 := sshtesting.ValidKeyOne.Key + " user@host"
	key2 := sshtesting.ValidKeyTwo.Key + " another@host"
	s.setAuthorisedKeys(c, key1, key2)

	context, err := coretesting.RunCommand(c, &ListKeysCommand{}, []string{})
	c.Assert(err, gc.IsNil)
	output := strings.TrimSpace(coretesting.Stdout(context))
	c.Assert(err, gc.IsNil)
	c.Assert(output, gc.Matches, "Keys for user admin:\n.*\\(user@host\\)\n.*\\(another@host\\)")
}

func (s *ListKeysSuite) TestListFullKeys(c *gc.C) {
	key1 := sshtesting.ValidKeyOne.Key + " user@host"
	key2 := sshtesting.ValidKeyTwo.Key + " another@host"
	s.setAuthorisedKeys(c, key1, key2)

	context, err := coretesting.RunCommand(c, &ListKeysCommand{}, []string{"--full"})
	c.Assert(err, gc.IsNil)
	output := strings.TrimSpace(coretesting.Stdout(context))
	c.Assert(err, gc.IsNil)
	c.Assert(output, gc.Matches, "Keys for user admin:\n.*user@host\n.*another@host")
}

func (s *ListKeysSuite) TestListKeysNonDefaultUser(c *gc.C) {
	key1 := sshtesting.ValidKeyOne.Key + " user@host"
	key2 := sshtesting.ValidKeyTwo.Key + " another@host"
	s.setAuthorisedKeys(c, key1, key2)
	_, err := s.State.AddUser("fred", "password")
	c.Assert(err, gc.IsNil)

	context, err := coretesting.RunCommand(c, &ListKeysCommand{}, []string{"--user", "fred"})
	c.Assert(err, gc.IsNil)
	output := strings.TrimSpace(coretesting.Stdout(context))
	c.Assert(err, gc.IsNil)
	c.Assert(output, gc.Matches, "Keys for user fred:\n.*\\(user@host\\)\n.*\\(another@host\\)")
}

func (s *ListKeysSuite) TestTooManyArgs(c *gc.C) {
	_, err := coretesting.RunCommand(c, &ListKeysCommand{}, []string{"foo"})
	c.Assert(err, gc.ErrorMatches, `unrecognized args: \["foo"\]`)
}

type AddKeySuite struct {
	keySuiteBase
}

var _ = gc.Suite(&AddKeySuite{})

func (s *AddKeySuite) TestAddKey(c *gc.C) {
	key1 := sshtesting.ValidKeyOne.Key + " user@host"
	s.setAuthorisedKeys(c, key1)

	key2 := sshtesting.ValidKeyTwo.Key + " another@host"
	context, err := coretesting.RunCommand(c, &AddKeysCommand{}, []string{key2, "invalid-key"})
	c.Assert(err, gc.IsNil)
	c.Assert(coretesting.Stderr(context), gc.Matches, `cannot add key "invalid-key".*\n`)
	s.assertEnvironKeys(c, key1, key2)
}

func (s *AddKeySuite) TestAddKeyNonDefaultUser(c *gc.C) {
	key1 := sshtesting.ValidKeyOne.Key + " user@host"
	s.setAuthorisedKeys(c, key1)
	_, err := s.State.AddUser("fred", "password")
	c.Assert(err, gc.IsNil)

	key2 := sshtesting.ValidKeyTwo.Key + " another@host"
	context, err := coretesting.RunCommand(c, &AddKeysCommand{}, []string{"--user", "fred", key2})
	c.Assert(err, gc.IsNil)
	c.Assert(coretesting.Stderr(context), gc.Equals, "")
	s.assertEnvironKeys(c, key1, key2)
}

type DeleteKeySuite struct {
	keySuiteBase
}

var _ = gc.Suite(&DeleteKeySuite{})

func (s *DeleteKeySuite) TestDeleteKeys(c *gc.C) {
	key1 := sshtesting.ValidKeyOne.Key + " user@host"
	key2 := sshtesting.ValidKeyTwo.Key + " another@host"
	s.setAuthorisedKeys(c, key1, key2)

	context, err := coretesting.RunCommand(
		c, &DeleteKeysCommand{}, []string{sshtesting.ValidKeyTwo.Fingerprint, "invalid-key"})
	c.Assert(err, gc.IsNil)
	c.Assert(coretesting.Stderr(context), gc.Matches, `cannot delete key id "invalid-key".*\n`)
	s.assertEnvironKeys(c, key1)
}

func (s *DeleteKeySuite) TestDeleteKeyNonDefaultUser(c *gc.C) {
	key1 := sshtesting.ValidKeyOne.Key + " user@host"
	key2 := sshtesting.ValidKeyTwo.Key + " another@host"
	s.setAuthorisedKeys(c, key1, key2)
	_, err := s.State.AddUser("fred", "password")
	c.Assert(err, gc.IsNil)

	context, err := coretesting.RunCommand(
		c, &DeleteKeysCommand{}, []string{"--user", "fred", sshtesting.ValidKeyTwo.Fingerprint})
	c.Assert(err, gc.IsNil)
	c.Assert(coretesting.Stderr(context), gc.Equals, "")
	s.assertEnvironKeys(c, key1)
}

type ImportKeySuite struct {
	keySuiteBase
}

var _ = gc.Suite(&ImportKeySuite{})

func (s *ImportKeySuite) SetUpTest(c *gc.C) {
	s.keySuiteBase.SetUpTest(c)
	s.PatchValue(&keymanagerserver.RunSSHImportId, keymanagertesting.FakeImport)
}

func (s *ImportKeySuite) TestImportKeys(c *gc.C) {
	key1 := sshtesting.ValidKeyOne.Key + " user@host"
	s.setAuthorisedKeys(c, key1)

	context, err := coretesting.RunCommand(c, &ImportKeysCommand{}, []string{"lp:validuser", "invalid-key"})
	c.Assert(err, gc.IsNil)
	c.Assert(coretesting.Stderr(context), gc.Matches, `cannot import key id "invalid-key".*\n`)
	s.assertEnvironKeys(c, key1, sshtesting.ValidKeyThree.Key)
}

func (s *ImportKeySuite) TestImportKeyNonDefaultUser(c *gc.C) {
	key1 := sshtesting.ValidKeyOne.Key + " user@host"
	s.setAuthorisedKeys(c, key1)
	_, err := s.State.AddUser("fred", "password")
	c.Assert(err, gc.IsNil)

	context, err := coretesting.RunCommand(c, &ImportKeysCommand{}, []string{"--user", "fred", "lp:validuser"})
	c.Assert(err, gc.IsNil)
	c.Assert(coretesting.Stderr(context), gc.Equals, "")
	s.assertEnvironKeys(c, key1, sshtesting.ValidKeyThree.Key)
}