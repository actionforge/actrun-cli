//go:build dev

package cmd

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/actionforge/actrun-cli/core"
	u "github.com/actionforge/actrun-cli/utils"

	// initialize all nodes
	_ "github.com/actionforge/actrun-cli/nodes"

	"github.com/spf13/cobra"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var cmdDevUpdateDatabase = &cobra.Command{
	Use:   "database",
	Short: "Update the registry in the database",
	Run: func(cmd *cobra.Command, args []string) {
		err := devUpdateDatabase()
		if err != nil {
			log.Fatal(err)
		}
	},
}

func init() {
	cmdDevUpdate.AddCommand(cmdDevUpdateDatabase)
}

func devUpdateDatabase() error {

	mongoDbUrl, _ := u.ResolveCliParam("mongodb_url", u.ResolveCliParamOpts{
		Flag:      true,
		Env:       true,
		ActPrefix: true,
	})

	mongoDbAuthSource, _ := u.ResolveCliParam("mongodb_auth_source", u.ResolveCliParamOpts{
		Flag:      true,
		Env:       true,
		ActPrefix: true,
	})

	mongoDbUsername, _ := u.ResolveCliParam("mongodb_username", u.ResolveCliParamOpts{
		Flag:      true,
		Env:       true,
		ActPrefix: true,
	})

	mongoDbPassword, _ := u.ResolveCliParam("mongodb_password", u.ResolveCliParamOpts{
		Flag:      true,
		Env:       true,
		ActPrefix: true,
	})

	m, err := CreateMongoDbClientWithCredentials(Root, mongoDbUrl, mongoDbUsername, mongoDbPassword, mongoDbAuthSource)
	if err != nil {
		panic(err)
	}
	defer m.Close()

	prod := os.Getenv("ACT_PROD") == "true"

	var registryDb *mongo.Database
	if prod {
		fmt.Println("Publishing to production database in 5 seconds...")
		fmt.Println("Type 'publish' to confirm: ")

		var input string
		fmt.Scanln(&input)
		if input != "publish" {
			fmt.Println("Aborted.")
			return nil
		}

		registryDb = m.Client.Database(MONGODB_DATABASE_NODES)
	} else {
		registryDb = m.Client.Database(MONGODB_DATABASE_NODES_DEV)
	}

	registryBuiltinsCol := registryDb.Collection(MONGODB_COLLECTION_BUILTINS)

	opts := options.Update().SetUpsert(true)

	fmt.Println("Updating registry...")

	for nodeId, nodeDef := range core.GetRegistries() {
		filter := bson.M{"_id": nodeId}

		fmt.Printf("  %s@v%v\n", nodeDef.Id, nodeDef.Version)
		// On insert or update, MongoDB combines the `filter` and `update` instructions. Due to
		// the `bson:"_id"` tag in NodeDef, MongoDB would receive two `_id`s, resulting in a failure.
		// I could nullify the `_id` in the node definition and add an `omitempty` tag to its field,
		// but to keep the sourec code changes local, I simply remove `_id` as it is already provided
		// by the `filter` from above.
		byteData, _ := bson.Marshal(nodeDef)
		var nodeDefMap bson.M
		err = bson.Unmarshal(byteData, &nodeDefMap)
		if err != nil {
			return err
		}
		delete(nodeDefMap, "_id") // already set by filter, see comment above

		update := bson.M{
			"$set": nodeDefMap,
		}
		_, err = registryBuiltinsCol.UpdateOne(context.Background(), filter, update, opts)
		if err != nil {
			return err
		}
	}
	return nil
}
