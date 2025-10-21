package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"time"

	envoyproxytypes "github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	resourcev3 "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	k8scache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
	gatewayinformers "sigs.k8s.io/gateway-api/pkg/client/informers/externalversions"

	"gateway-xds-generator/pkg/translator"
)

var (
	gatewayName = flag.String("gateway", "", "Name of the Gateway resource")
	gatewayNs   = flag.String("namespace", "default", "Namespace of the Gateway resource")
	outputFile  = flag.String("output", "envoy-xds.json", "Output file for the Envoy XDS configuration")
)

func main() {
	flag.Parse()

	if *gatewayName == "" || *gatewayNs == "" {
		fmt.Println("Error: --gateway and --namespace are required")
		os.Exit(1)
	}

	usr, err := user.Current()
	if err != nil {
		fmt.Printf("Failed to get current user: %v\n", err)
		os.Exit(1)
	}
	kubeconfig := filepath.Join(usr.HomeDir, ".kube", "config")
	// Build Kubernetes config
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		fmt.Printf("Error building kubeconfig: %v\n", err)
		os.Exit(1)
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Printf("Error creating kube client: %v\n", err)
	}
	// Create Gateway API clientset
	gatewayClientset, err := gatewayclient.NewForConfig(config)
	if err != nil {
		fmt.Printf("Error creating Gateway API clientset: %v\n", err)
		os.Exit(1)
	}

	// Fetch Gateway resource
	gw, err := gatewayClientset.GatewayV1().Gateways(*gatewayNs).Get(context.Background(), *gatewayName, metav1.GetOptions{})
	if err != nil {
		fmt.Printf("Error fetching Gateway %s/%s: %v\n", *gatewayNs, *gatewayName, err)
		os.Exit(1)
	}

	fmt.Printf("Fetched Gateway: %s/%s\n", gw.Namespace, gw.Name)

	sharedInformers := informers.NewSharedInformerFactory(kubeClient, 60*time.Second)
	sharedGwInformers := gatewayinformers.NewSharedInformerFactory(gatewayClientset, 60*time.Second)

	stopCh := make(chan struct{})
	defer close(stopCh)
	go sharedGwInformers.Start(stopCh)
	go sharedInformers.Start(stopCh)

	hasSynced := []k8scache.InformerSynced{
		// sharedInformers.Core().V1().Namespaces().Informer().HasSynced,
		sharedInformers.Core().V1().Services().Informer().HasSynced,
		// sharedInformers.Core().V1().Secrets().Informer().HasSynced,
		sharedGwInformers.Gateway().V1().Gateways().Informer().HasSynced,
		sharedGwInformers.Gateway().V1().HTTPRoutes().Informer().HasSynced,
		// sharedGwInformers.Gateway().V1beta1().ReferenceGrants().Informer().HasSynced,
	}
	k8scache.WaitForNamedCacheSync("test", stopCh, hasSynced...)

	// Initialize translator
	translator := translator.New(
		kubeClient,
		gatewayClientset,
		sharedInformers.Core().V1().Namespaces().Lister(),
		sharedInformers.Core().V1().Services().Lister(),
		sharedInformers.Core().V1().Secrets().Lister(),
		sharedGwInformers.Gateway().V1().Gateways().Lister(),
		sharedGwInformers.Gateway().V1().HTTPRoutes().Lister(),
		sharedGwInformers.Gateway().V1beta1().ReferenceGrants().Lister(),
	)

	// Translate Gateway and HTTPRoute to Envoy XDS
	resources, err := translator.TranslateGatewayToXDS(context.Background(), gw)
	if err != nil {
		fmt.Printf("Error translating Gateway to XDS: %v\n", err)
		os.Exit(1)
	}

	snapshot, err := generateXDS(resources)
	if err != nil {
		fmt.Printf("Error generating XDS: %v\n", err)
		os.Exit(1)
	}
	if err := snapshot.Consistent(); err != nil {
		fmt.Printf("Snapshot is inconsistent: %v\n", err)
		os.Exit(1)
	}

	// Serialize snapshot to JSON
	xdsJSON, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling XDS to JSON: %v\n", err)
		os.Exit(1)
	}
	// Write XDS to output file
	err = os.WriteFile(*outputFile, xdsJSON, 0644)
	if err != nil {
		fmt.Printf("Error writing to output file %s: %v\n", *outputFile, err)
		os.Exit(1)
	}

	fmt.Printf("Successfully wrote XDS to %s\n", *outputFile)
}

func generateXDS(resources map[resourcev3.Type][]envoyproxytypes.Resource) (*cache.Snapshot, error) {
	version := time.Now().Format(time.RFC3339Nano)
	snapshot, err := cache.NewSnapshot(version, resources)
	if err != nil {
		fmt.Printf("Error generating snapshot: %v\n", err)
		os.Exit(1)
	}

	return snapshot, nil
}
