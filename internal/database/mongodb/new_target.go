package mongodb

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"net"
	"strconv"
	"time"
)

func (a *Adapter) ForFullBackup() database.Adapter {
	b := *a
	b.Config.Options = config.Options{Quiesced: a.Config.Options.Quiesced}
	return &b
}

func (a *Adapter) targetClient() (*mongo.Client, error) {
	auth := a.Config.AuthDatabase
	if auth == "" {
		auth = "admin"
	}
	o := options.Client().SetHosts([]string{net.JoinHostPort(a.Config.Host, strconv.Itoa(a.Config.Port))}).SetAuth(options.Credential{Username: a.Config.User, Password: a.Password, AuthSource: auth}).SetServerSelectionTimeout(10 * time.Second).SetConnectTimeout(10 * time.Second)
	if a.Config.SSLMode == "require" || a.Config.SSLMode == "verify-full" {
		o.SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})
	}
	return mongo.Connect(o)
}
func closeTargetClient(ctx context.Context, c *mongo.Client) {
	x, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	c.Disconnect(x)
}
func (a *Adapter) PreflightNew(ctx context.Context) (database.Info, error) {
	if err := database.ValidateNewName(a.Config.Database); err != nil {
		return database.Info{}, err
	}
	info, err := a.Preflight(ctx)
	if err != nil {
		return info, err
	}
	return info, a.checkAbsent(ctx)
}
func (a *Adapter) checkAbsent(ctx context.Context) error {
	if err := database.ValidateNewName(a.Config.Database); err != nil {
		return err
	}
	c, err := a.targetClient()
	if err != nil {
		return err
	}
	defer closeTargetClient(ctx, c)
	names, err := c.ListDatabaseNames(ctx, bson.D{{Key: "name", Value: a.Config.Database}})
	if err == nil && len(names) > 0 {
		err = fmt.Errorf("destination database already exists; choose a different name")
	}
	return err
}

// MongoDB creates databases lazily. Recheck absence immediately before restore;
// operators must prevent concurrent creation/writes to the destination namespace.
func (a *Adapter) CreateNew(ctx context.Context) error { return a.checkAbsent(ctx) }
