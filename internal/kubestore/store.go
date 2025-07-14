package kubestore

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"tailscale.com/ipn"
	"tailscale.com/types/logger"
)

type Store struct {
	logf       logger.Logf
	secretName string
	client     kubernetes.Interface
	namespace  string
	mu         sync.RWMutex
	cache      map[ipn.StateKey][]byte
}

func New(logf logger.Logf, secretName string) (*Store, error) {
	if logf == nil {
		logf = log.Printf
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	namespace, err := getCurrentNamespace()
	if err != nil {
		return nil, fmt.Errorf("failed to get current namespace: %w", err)
	}

	store := &Store{
		logf:       logf,
		secretName: secretName,
		client:     clientset,
		namespace:  namespace,
		cache:      make(map[ipn.StateKey][]byte),
	}

	if err := store.loadFromSecret(); err != nil {
		store.logf("Failed to load existing state from secret, starting fresh: %v", err)
	}

	return store, nil
}

func (s *Store) ReadState(key ipn.StateKey) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, exists := s.cache[key]
	if !exists {
		return nil, ipn.ErrStateNotExist
	}
	return data, nil
}

func (s *Store) WriteState(key ipn.StateKey, data []byte) error {
	s.mu.Lock()
	s.cache[key] = data
	s.mu.Unlock()

	return s.updateSecret(map[string][]byte{string(key): data})
}

func (s *Store) loadFromSecret() error {
	ctx := context.Background()
	secret, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, s.secretName, metav1.GetOptions{})
	if err != nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for key, value := range secret.Data {
		s.cache[ipn.StateKey(key)] = value
	}

	return nil
}

func (s *Store) updateSecret(updates map[string][]byte) error {
	ctx := context.Background()

	s.mu.RLock()
	data := make(map[string][]byte)
	for key, value := range s.cache {
		data[string(key)] = value
	}
	s.mu.RUnlock()

	for key, value := range updates {
		data[key] = value
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.secretName,
			Namespace: s.namespace,
		},
		Data: data,
	}

	_, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, s.secretName, metav1.GetOptions{})
	if err != nil {
		_, err = s.client.CoreV1().Secrets(s.namespace).Create(ctx, secret, metav1.CreateOptions{})
		return err
	}

	_, err = s.client.CoreV1().Secrets(s.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	return err
}

func getCurrentNamespace() (string, error) {
	namespaceBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "", fmt.Errorf("failed to read namespace: %w", err)
	}
	return strings.TrimSpace(string(namespaceBytes)), nil
}

func NewFromConfig(logf logger.Logf, stateConfig string) (ipn.StateStore, error) {
	if !strings.HasPrefix(stateConfig, "kube:") {
		return nil, fmt.Errorf("invalid state config format, expected 'kube:<secret-name>'")
	}

	secretName := strings.TrimPrefix(stateConfig, "kube:")
	if secretName == "" {
		return nil, fmt.Errorf("empty secret name in state config")
	}

	return New(logf, secretName)
}
