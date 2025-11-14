package pggeo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func Connect(ctx context.Context, user, password, host, port, dbname string) (*pgx.Conn, error) {
	conn, err := pgx.Connect(ctx, fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s", host, port, user, password, dbname))
	if err != nil {
		return nil, err
	}
	return conn, nil
}
