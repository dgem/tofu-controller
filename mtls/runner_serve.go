package mtls

import (
	"context"
	"fmt"
	"net"
	"os"

	infrav1 "github.com/flux-iac/tofu-controller/api/v1alpha2"
	"github.com/flux-iac/tofu-controller/runner"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"google.golang.org/grpc"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func RunnerServe(namespace, addr string, tlsSecretName string, sigterm chan os.Signal, maxMessageSizeInMiB int) error {
	scheme := runtime.NewScheme()

	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return err
	}
	if err := sourcev1.AddToScheme(scheme); err != nil {
		return err
	}
	if err := infrav1.AddToScheme(scheme); err != nil {
		return err
	}

	cfg := controllerruntime.GetConfigOrDie()
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	if k8sClient == nil {
		return fmt.Errorf("k8sClient cannot be nil")
	}

	// local runner, use the same client as the manager
	runnerServer := &runner.TerraformRunnerServer{
		Client: k8sClient,
		Scheme: scheme,
		Done:   sigterm,
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	secretKey := types.NamespacedName{Namespace: namespace, Name: tlsSecretName}
	// TODO watch this Secret, then restart the server if the Secret is changed
	tlsSecret := &v1.Secret{}
	if err := k8sClient.Get(context.Background(), secretKey, tlsSecret); err != nil {
		return err
	}

	credentials, err := GetGRPCServerCredentials(tlsSecret)
	if err != nil {
		return err
	}

	// 30 MB is the maximum allowed payload size for gRPC.
	maxMsgSize := maxMessageSizeInMiB * 1024 * 1024
	grpcServer := grpc.NewServer(grpc.Creds(credentials), grpc.MaxRecvMsgSize(maxMsgSize), grpc.MaxSendMsgSize(maxMsgSize))
	runner.RegisterRunnerServer(grpcServer, runnerServer)

	if err := grpcServer.Serve(listener); err != nil {
		return err
	}

	return nil
}
