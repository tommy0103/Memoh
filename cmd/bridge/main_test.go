package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/stats"

	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
)

type blockingExecBridgeServer struct {
	pb.UnimplementedContainerServiceServer
	started chan struct{}
	release chan struct{}
}

func (s *blockingExecBridgeServer) Exec(pb.ContainerService_ExecServer) error {
	close(s.started)
	// Deliberately ignore stream cancellation. Positive-timeout bridge Exec
	// handlers can do the same while their child process is still running.
	<-s.release
	return nil
}

func TestBridgeGRPCShutdownStopsActiveStreams(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	service := &blockingExecBridgeServer{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	defer close(service.release)
	server := grpc.NewServer()
	pb.RegisterContainerServiceServer(server, service)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()

	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		server.Stop()
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	streamCtx, streamCancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer streamCancel()
	stream, err := pb.NewContainerServiceClient(conn).Exec(streamCtx)
	if err != nil {
		server.Stop()
		t.Fatalf("open exec stream: %v", err)
	}
	if err := stream.Send(&pb.ExecInput{Command: "sleep forever"}); err != nil {
		server.Stop()
		t.Fatalf("start exec stream: %v", err)
	}
	select {
	case <-service.started:
	case <-time.After(time.Second):
		server.Stop()
		t.Fatal("exec stream did not start")
	}

	ctx, cancel := context.WithCancel(context.Background())
	shutdownDone := make(chan struct{})
	go func() {
		stopBridgeGRPCServer(ctx, slog.New(slog.DiscardHandler), server)
		close(shutdownDone)
	}()
	cancel()

	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		server.Stop()
		t.Fatal("bridge shutdown did not finish")
	}
	if _, err := stream.Recv(); err == nil {
		t.Fatal("active exec stream survived forced bridge shutdown")
	}
	select {
	case err := <-serveDone:
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("gRPC Serve did not return after shutdown")
	}
}

// Observe real server transports so the test distinguishes reuse from silently
// opening another connection while the original stream is still running.
type bridgeConnectionStats struct {
	opened atomic.Int32
	closed chan struct{}
}

func (*bridgeConnectionStats) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	return ctx
}

func (*bridgeConnectionStats) HandleRPC(context.Context, stats.RPCStats) {}

func (*bridgeConnectionStats) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

func (s *bridgeConnectionStats) HandleConn(_ context.Context, event stats.ConnStats) {
	switch event.(type) {
	case *stats.ConnBegin:
		s.opened.Add(1)
	case *stats.ConnEnd:
		s.closed <- struct{}{}
	}
}

func (*blockingExecBridgeServer) Stat(context.Context, *pb.StatRequest) (*pb.StatResponse, error) {
	return &pb.StatResponse{}, nil
}

func TestBridgeReusesActiveConnectionAndReclaimsItWhenIdle(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	params := bridgeKeepaliveParameters()
	params.MaxConnectionIdle = 100 * time.Millisecond
	if params.MaxConnectionAge != 0 {
		// Accelerate a mistakenly reintroduced age limit so this test can
		// detect transport rotation without waiting thirty minutes.
		params.MaxConnectionAge = 50 * time.Millisecond
	}
	if params.MaxConnectionAgeGrace != 0 {
		params.MaxConnectionAgeGrace = 50 * time.Millisecond
	}
	service := &blockingExecBridgeServer{started: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(service.release) }) }
	defer release()
	observed := &bridgeConnectionStats{closed: make(chan struct{}, 16)}
	server := grpc.NewServer(grpc.KeepaliveParams(params), grpc.StatsHandler(observed))
	pb.RegisterContainerServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client := pb.NewContainerServiceClient(conn)
	stream, err := client.Exec(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&pb.ExecInput{Command: "hold active connection"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-service.started:
	case <-ctx.Done():
		t.Fatal("stream did not start")
	}

	// A quiet but active stream is still in use. It must survive several
	// idle intervals, and another RPC must share the same transport.
	select {
	case <-observed.closed:
		t.Fatal("connection closed while an RPC was active")
	case <-time.After(3 * params.MaxConnectionIdle):
	}
	if _, err := client.Stat(ctx, &pb.StatRequest{Path: "/tmp"}); err != nil {
		t.Fatal(err)
	}
	if n := observed.opened.Load(); n != 1 {
		t.Fatalf("active connection was rotated: opened %d transports", n)
	}
	release()
	if _, err := stream.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("active stream did not finish normally: %v", err)
	}
	select {
	case <-observed.closed:
	case <-ctx.Done():
		t.Fatal("unused connection was not reclaimed after the idle timeout")
	}
	if _, err := client.Stat(ctx, &pb.StatRequest{Path: "/tmp"}); err != nil {
		t.Fatalf("RPC after idle reclamation failed: %v", err)
	}
	if n := observed.opened.Load(); n < 2 {
		t.Fatalf("RPC after idle reclamation did not open a new transport: %d", n)
	}
}
