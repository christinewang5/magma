package handlers

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"magma/orc8r/cloud/go/services/certifier"
	certprotos "magma/orc8r/cloud/go/services/certifier/protos"
	"magma/orc8r/cloud/go/tools/commands"
)

func init() {
	cmd := CommandRegistry.Add(
		"add-admin-token",
		"Add a new admin which has all permissions to read/write all entities and create a new admin token",
		addAdminToken)

	f := cmd.Flags()
	f.Usage = func() {
		fmt.Fprintf(os.Stderr, // std Usage() & PrintDefaults() use Stderr
			"\tUsage: %s %s [OPTIONS] <Admin Username> <Admin Password>\n", os.Args[0], cmd.Name())
		f.PrintDefaults()
	}

	addInit(f) // see common_add.go
}

func addAdminToken(cmd *commands.Command, args []string) int {
	f := cmd.Flags()
	username := strings.TrimSpace(f.Arg(0))
	password := strings.TrimSpace(f.Arg(1))

	if f.NArg() != 2 || len(username) == 0 || len(password) == 0 {
		f.Usage()
		log.Fatalf("The admin username and password must be provided.")
	}

	token, _ := certifier.GenerateToken(certifier.Personal)
	user := &certprotos.User{
		Username: username,
		Password: []byte(password),
		Tokens: &certprotos.TokenList{
			Tokens: []string{token},
		},
	}
	// resource := &certprotos.Resource{
	// 	Effect:       certprotos.Effect_ALLOW,
	// 	Action:       certprotos.Action_WRITE,
	// 	ResourceType: certprotos.ResourceType_URI,
	// 	Resource:     "/**",
	// }
	// policy := &certprotos.Policy{
	// 	Token: token,
	// 	Resources: &certprotos.ResourceList{
	// 		Resources: []*certprotos.Resource{resource},
	// 	},
	// }
	ctx := context.Background()
	err := certifier.CreateUser(ctx, user)
	// TODO(christinewang5): use handler to register a token for admin
	if err != nil {
		panic("Failed to create admin token")
	}

	return 0
}
