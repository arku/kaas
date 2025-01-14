package kaas

import (
	"context"
	"log"
	"net"

	"github.com/RTradeLtd/rtfs/v2/krab"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_ctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
	ci "github.com/libp2p/go-libp2p-crypto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/RTradeLtd/config/v2"
	pb "github.com/RTradeLtd/grpc/krab"
	"github.com/RTradeLtd/grpc/middleware"
)

// Server is the backend for Krab
type Server struct {
	krab   *krab.Krab
	server *grpc.Server
	pb.ServiceServer
}

// NewServer is used to create, and run a krab keystore server
func NewServer(listenAddr, protocol string, cfg *config.TemporalConfig) error {
	lis, err := net.Listen(protocol, listenAddr)
	if err != nil {
		return err
	}
	// setup authentication interceptor
	unaryIntercept, streamInterceptor := middleware.NewServerInterceptors(cfg.Services.Krab.AuthKey)
	// setup server options
	serverOpts := []grpc.ServerOption{
		grpc_middleware.WithUnaryServerChain(
			unaryIntercept,
			grpc_ctxtags.UnaryServerInterceptor(grpc_ctxtags.WithFieldExtractor(grpc_ctxtags.CodeGenRequestFieldExtractor))),
		grpc_middleware.WithStreamServerChain(
			streamInterceptor,
			grpc_ctxtags.StreamServerInterceptor(grpc_ctxtags.WithFieldExtractor(grpc_ctxtags.CodeGenRequestFieldExtractor))),
	}
	// setup tls configuration if available
	if cfg.Services.Krab.TLS.CertPath != "" {
		creds, err := credentials.NewServerTLSFromFile(
			cfg.Krab.TLS.CertPath,
			cfg.Krab.TLS.KeyFile,
		)
		if err != nil {
			return err
		}
		serverOpts = append(serverOpts, grpc.Creds(creds))
	}
	// create grpc server
	gServer := grpc.NewServer(serverOpts...)
	// setup krab backend
	kb, err := krab.NewKrab(krab.Opts{
		Passphrase: cfg.Services.Krab.KeystorePassword,
		DSPath:     cfg.IPFS.KeystorePath,
		ReadOnly:   false},
	)
	if err != nil {
		return err
	}
	server := &Server{
		krab:   kb,
		server: gServer,
	}
	pb.RegisterServiceServer(gServer, server)
	// defer closing of all services
	defer func() {
		if err := server.close(); err != nil {
			log.Fatal(err)
		}
	}()
	return gServer.Serve(lis)
}

// GetPrivateKey is used to retrieve a private key by searching for its name
func (s *Server) GetPrivateKey(ctx context.Context, req *pb.KeyGet) (*pb.Response, error) {
	pk, err := s.krab.Get(req.Name)
	if err != nil {
		return nil, err
	}
	keyBytes, err := pk.Bytes()
	if err != nil {
		return nil, err
	}
	return &pb.Response{
		Status:     "private key retrieved",
		PrivateKey: keyBytes,
	}, nil
}

// PutPrivateKey is used to store a new private key
func (s *Server) PutPrivateKey(ctx context.Context, req *pb.KeyPut) (*pb.Response, error) {
	pk, err := ci.UnmarshalPrivateKey(req.PrivateKey)
	if err != nil {
		return nil, err
	}
	if err := s.krab.Put(req.Name, pk); err != nil {
		return nil, err
	}
	return &pb.Response{
		Status: "private key stored",
	}, nil
}

// DeletePrivateKey is used to remove a private key from the keystore
func (s *Server) DeletePrivateKey(ctx context.Context, req *pb.KeyDelete) (*pb.Response, error) {
	if err := s.krab.Delete(req.Name); err != nil {
		return nil, err
	}
	return &pb.Response{
		Status: "private key deleted",
	}, nil
}

// ListPrivateKeys is used to return a list of private key ID's of all keys in our keystore
func (s *Server) ListPrivateKeys(ctx context.Context, req *pb.KeyList) (*pb.Response, error) {
	keyIDs, err := s.krab.List()
	if err != nil {
		return nil, err
	}
	return &pb.Response{
		Status: "keys fetched",
		KeyIDs: keyIDs,
	}, nil
}

// Close is used to gracefully stop any grpc connections, followed by closing the datastore
func (s *Server) close() error {
	// gracefull store any pending connections first
	s.server.GracefulStop()
	// now close the datastore
	return s.krab.Close()
}
