// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build ignore

package main

import (
	"context"
	"fmt"
	"time"

	sdklog "go.opentelemetry.io/otel/sdk/log"
)

func main() {
	ctx := context.Background()

	memoryExample(ctx)
	fmt.Println()
	redisExample(ctx)
	fmt.Println()
	fileExample(ctx)
	fmt.Println()
	sqlExample(ctx)
}

func memoryExample(ctx context.Context) {
	fmt.Println("=== Memory Storage Extension Example ===")

	extension, err := sdklog.NewStorageExtension(
		sdklog.WithMemoryStorage(),
	)
	if err != nil {
		panic(err)
	}

	if err := extension.Start(ctx); err != nil {
		panic(err)
	}
	defer extension.Shutdown(ctx)

	client, err := extension.GetClient(ctx, "audit_logs")
	if err != nil {
		panic(err)
	}

	adapter, err := sdklog.NewAuditLogStorageExtensionAdapter(client)
	if err != nil {
		panic(err)
	}

	fmt.Println("✅ Memory storage extension created")
	fmt.Println("   Type: In-Memory (map)")
	fmt.Println("   Use Case: Testing, development")
	fmt.Printf("   Adapter: %T\n", adapter)
}

func redisExample(ctx context.Context) {
	fmt.Println("=== Redis Storage Extension Example ===")

	extension, err := sdklog.NewStorageExtension(
		sdklog.WithRedisStorage(
			"localhost:6379",
			sdklog.WithRedisPrefix("audit_"),
			sdklog.WithRedisDB(0),
			sdklog.WithRedisExpiration(24*time.Hour),
		),
	)
	if err != nil {
		fmt.Printf("⚠️  Failed to create Redis extension: %v\n", err)
		fmt.Println("   (Make sure Redis is running: docker run -d -p 6379:6379 redis:latest)")
		return
	}

	if err := extension.Start(ctx); err != nil {
		panic(err)
	}
	defer extension.Shutdown(ctx)

	client1, err := extension.GetClient(ctx, "component1")
	if err != nil {
		panic(err)
	}

	client2, err := extension.GetClient(ctx, "component2")
	if err != nil {
		panic(err)
	}

	adapter1, _ := sdklog.NewAuditLogStorageExtensionAdapter(client1)
	adapter2, _ := sdklog.NewAuditLogStorageExtensionAdapter(client2)

	fmt.Println("✅ Redis storage extension created")
	fmt.Println("   Type: Redis")
	fmt.Println("   Endpoint: localhost:6379")
	fmt.Println("   Use Case: Distributed systems, multi-instance")
	fmt.Printf("   Client 1: %T (component1)\n", adapter1)
	fmt.Printf("   Client 2: %T (component2)\n", adapter2)
	fmt.Println("   Note: Each client gets isolated storage with prefix")
}

func fileExample(ctx context.Context) {
	fmt.Println("=== File Storage Extension Example ===")

	extension, err := sdklog.NewStorageExtension(
		sdklog.WithFileStorage("./storage"),
	)
	if err != nil {
		panic(err)
	}

	if err := extension.Start(ctx); err != nil {
		panic(err)
	}
	defer extension.Shutdown(ctx)

	client, err := extension.GetClient(ctx, "audit_logs")
	if err != nil {
		panic(err)
	}

	adapter, err := sdklog.NewAuditLogStorageExtensionAdapter(client)
	if err != nil {
		panic(err)
	}

	fmt.Println("✅ File storage extension created")
	fmt.Println("   Type: BoltDB (file-based)")
	fmt.Println("   Directory: ./storage")
	fmt.Println("   Use Case: Single-node production, persistence")
	fmt.Printf("   Adapter: %T\n", adapter)
}

func sqlExample(ctx context.Context) {
	fmt.Println("=== SQL Storage Extension Example ===")

	extension, err := sdklog.NewStorageExtension(
		sdklog.WithSQLStorage(
			"sqlite3",
			":memory:",
			sdklog.WithSQLTableName("audit_storage"),
		),
	)
	if err != nil {
		panic(err)
	}

	if err := extension.Start(ctx); err != nil {
		panic(err)
	}
	defer extension.Shutdown(ctx)

	client, err := extension.GetClient(ctx, "audit_logs")
	if err != nil {
		panic(err)
	}

	adapter, err := sdklog.NewAuditLogStorageExtensionAdapter(client)
	if err != nil {
		panic(err)
	}

	fmt.Println("✅ SQL storage extension created")
	fmt.Println("   Type: SQL Database (SQLite)")
	fmt.Println("   Driver: sqlite3")
	fmt.Println("   Use Case: Enterprise, compliance, SQL queries")
	fmt.Printf("   Adapter: %T\n", adapter)
}

