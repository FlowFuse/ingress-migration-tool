// Package main provides a CLI for migrating Kubernetes Ingress resources.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Config defines runtime settings for the ingress migration workflow.
type Config struct {
	Namespace       string
	Mode            string
	OldIngressClass string
	NewIngressClass string
	NameSuffix      string
	DryRun          bool
	Concurrency     int
}

func main() {
	config := loadConfig()

	log.Println("==========================================================")
	log.Printf("Ingress Migration Job Started (MODE=%s)", config.Mode)
	log.Printf("  Namespace: %s", config.Namespace)
	if config.Mode == "cleanup" && config.OldIngressClass != "" {
		log.Printf("  Old IngressClassName: %s", config.OldIngressClass)
	}
	log.Printf("  New IngressClassName: %s", config.NewIngressClass)
	log.Printf("  Name Suffix: %s", config.NameSuffix)
	log.Printf("  Concurrency: %d", config.Concurrency)
	log.Printf("  Dry Run: %v", config.DryRun)
	log.Println("==========================================================")

	clientset, err := getKubernetesClient()
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	switch config.Mode {
	case "copy":
		if err := copyIngressObjects(clientset, config); err != nil {
			log.Fatalf("Failed to copy ingress objects: %v", err)
		}
		log.Println("Ingress Copy Job Completed Successfully")
	case "cleanup":
		if err := cleanupIngressObjects(clientset, config); err != nil {
			log.Fatalf("Failed to cleanup ingress objects: %v", err)
		}
		log.Println("Ingress Cleanup Job Completed Successfully")
	default:
		log.Fatalf("Invalid MODE='%s'. Valid modes: 'copy', 'cleanup'", config.Mode)
	}
}

func loadConfig() Config {
	namespace := getEnv("TARGET_NAMESPACE", "default")
	mode := getEnv("MODE", "copy")
	oldIngressClass := os.Getenv("OLD_INGRESS_CLASS")
	newIngressClass := os.Getenv("NEW_INGRESS_CLASS")
	nameSuffix := getEnv("NAME_SUFFIX", "-copy")
	dryRun, _ := strconv.ParseBool(getEnv("DRY_RUN", "false"))
	concurrency, _ := strconv.Atoi(getEnv("CONCURRENCY", "100"))

	if newIngressClass == "" {
		log.Fatal("NEW_INGRESS_CLASS environment variable must be set")
	}

	if mode == "cleanup" && oldIngressClass == "" {
		log.Fatal("OLD_INGRESS_CLASS environment variable must be set for cleanup mode")
	}

	if concurrency < 1 {
		concurrency = 50
	}

	return Config{
		Namespace:       namespace,
		Mode:            mode,
		OldIngressClass: oldIngressClass,
		NewIngressClass: newIngressClass,
		NameSuffix:      nameSuffix,
		DryRun:          dryRun,
		Concurrency:     concurrency,
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getKubernetesClient() (*kubernetes.Clientset, error) {
	// Try in-cluster config first (when running in a pod)
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig for local testing
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			home, _ := os.UserHomeDir()
			kubeconfig = fmt.Sprintf("%s/.kube/config", home)
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
		}
	}

	// Increase client-side rate limits for high-concurrency operations
	// Default QPS=5, Burst=10 is too low for bulk ingress creation
	config.QPS = 50.0  // Queries per second
	config.Burst = 100 // Maximum burst for throttle

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	return clientset, nil
}

func copyIngressObjects(clientset *kubernetes.Clientset, config Config) error {
	ctx := context.Background()

	// List all ingress objects in the namespace
	ingresses, err := clientset.NetworkingV1().Ingresses(config.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list ingresses: %w", err)
	}

	// Filter ingresses that need to be copied
	var ingressesToCopy []networkingv1.Ingress
	skippedCount := 0

	for _, ingress := range ingresses.Items {
		originalName := ingress.Name

		// Skip if this ingress already has the target ingressClassName
		if ingress.Spec.IngressClassName != nil && *ingress.Spec.IngressClassName == config.NewIngressClass {
			log.Printf("Skipping '%s' - already uses ingressClassName '%s'", originalName, config.NewIngressClass)
			skippedCount++
			continue
		}

		// Skip if this looks like a previously copied ingress
		if strings.HasSuffix(originalName, config.NameSuffix) {
			log.Printf("Skipping '%s' - appears to be a copy", originalName)
			skippedCount++
			continue
		}

		ingressesToCopy = append(ingressesToCopy, ingress)
	}

	if len(ingressesToCopy) == 0 {
		log.Printf("No ingresses to copy. Summary: 0 copied, %d skipped", skippedCount)
		return nil
	}

	log.Printf("Processing %d ingresses with concurrency=%d", len(ingressesToCopy), config.Concurrency)

	// Process ingresses concurrently
	var copiedCount int32
	var failedCount int32
	var skippedInCopy int32
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, config.Concurrency)
	errChan := make(chan error, len(ingressesToCopy))

	for _, ingress := range ingressesToCopy {
		wg.Add(1)
		go func(ing networkingv1.Ingress) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			originalName := ing.Name
			newName := originalName + config.NameSuffix

			currentClass := "<none>"
			if ing.Spec.IngressClassName != nil {
				currentClass = *ing.Spec.IngressClassName
			}

			log.Printf("Copying '%s' -> '%s' (class: '%s' -> '%s')",
				originalName, newName, currentClass, config.NewIngressClass)

			if config.DryRun {
				log.Printf("[DRY RUN] Would create ingress '%s'", newName)
				atomic.AddInt32(&copiedCount, 1)
				return
			}

			// Create new ingress object
			newIngress := createNewIngress(&ing, newName, config)

			_, err := clientset.NetworkingV1().Ingresses(config.Namespace).Create(ctx, newIngress, metav1.CreateOptions{})
			if err != nil {
				if strings.Contains(err.Error(), "already exists") {
					log.Printf("Warning: Ingress '%s' already exists - skipping", newName)
					atomic.AddInt32(&skippedInCopy, 1)
				} else {
					log.Printf("ERROR: Failed to create ingress '%s': %v", newName, err)
					atomic.AddInt32(&failedCount, 1)
					errChan <- fmt.Errorf("failed to create ingress '%s': %w", newName, err)
				}
			} else {
				log.Printf("Successfully created ingress '%s'", newName)
				atomic.AddInt32(&copiedCount, 1)
			}
		}(ingress)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errChan)

	// Check for errors
	if failedCount > 0 {
		var firstErr error
		for err := range errChan {
			if firstErr == nil {
				firstErr = err
			}
		}
		log.Printf("Summary: %d copied, %d pre-filtered skipped, %d already-exist skipped, %d failed",
			copiedCount, skippedCount, skippedInCopy, failedCount)
		return fmt.Errorf("failed to copy %d ingresses (first error: %w)", failedCount, firstErr)
	}

	log.Printf("Summary: %d copied, %d pre-filtered skipped, %d already-exist skipped",
		copiedCount, skippedCount, skippedInCopy)
	return nil
}

func cleanupIngressObjects(clientset *kubernetes.Clientset, config Config) error {
	ctx := context.Background()

	// List all ingress objects in the namespace
	ingresses, err := clientset.NetworkingV1().Ingresses(config.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list ingresses: %w", err)
	}

	if len(ingresses.Items) == 0 {
		log.Printf("No ingresses found in namespace '%s'", config.Namespace)
		return nil
	}

	log.Printf("Found %d Ingress object(s) in namespace '%s'", len(ingresses.Items), config.Namespace)

	// Separate ingresses into categories
	var oldIngresses []networkingv1.Ingress         // Old controller ingresses to delete
	var newSuffixedIngresses []networkingv1.Ingress // New controller with suffix to rename

	for _, ingress := range ingresses.Items {
		ingressClass := "<none>"
		if ingress.Spec.IngressClassName != nil {
			ingressClass = *ingress.Spec.IngressClassName
		}

		// Check if it's an old ingress to delete
		if config.OldIngressClass != "" && ingressClass == config.OldIngressClass {
			log.Printf("Found old ingress to delete: '%s' (class: %s)", ingress.Name, ingressClass)
			oldIngresses = append(oldIngresses, ingress)
			continue
		}

		// Check if it's a new ingress with suffix to rename
		if ingressClass == config.NewIngressClass && strings.HasSuffix(ingress.Name, config.NameSuffix) {
			log.Printf("Found new ingress to rename: '%s' (class: %s)", ingress.Name, ingressClass)
			newSuffixedIngresses = append(newSuffixedIngresses, ingress)
			continue
		}
	}

	log.Printf("Cleanup plan: Delete %d old ingresses, Rename %d new ingresses",
		len(oldIngresses), len(newSuffixedIngresses))

	if len(oldIngresses) == 0 && len(newSuffixedIngresses) == 0 {
		log.Println("No ingresses to cleanup. Summary: 0 deleted, 0 renamed")
		return nil
	}

	// Process cleanup concurrently
	var deletedCount int32
	var renamedCount int32
	var failedCount int32
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, config.Concurrency)
	errChan := make(chan error, len(oldIngresses)+len(newSuffixedIngresses))

	// Delete old ingresses
	for _, ingress := range oldIngresses {
		wg.Add(1)
		go func(ing networkingv1.Ingress) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			log.Printf("Deleting old ingress '%s'...", ing.Name)

			if config.DryRun {
				log.Printf("[DRY RUN] Would delete ingress '%s'", ing.Name)
				atomic.AddInt32(&deletedCount, 1)
				return
			}

			err := clientset.NetworkingV1().Ingresses(config.Namespace).Delete(ctx, ing.Name, metav1.DeleteOptions{})
			if err != nil {
				log.Printf("ERROR: Failed to delete ingress '%s': %v", ing.Name, err)
				atomic.AddInt32(&failedCount, 1)
				errChan <- fmt.Errorf("failed to delete ingress '%s': %w", ing.Name, err)
			} else {
				log.Printf("Successfully deleted old ingress '%s'", ing.Name)
				atomic.AddInt32(&deletedCount, 1)
			}
		}(ingress)
	}

	// Rename new ingresses (by creating without suffix and deleting with suffix)
	for _, ingress := range newSuffixedIngresses {
		wg.Add(1)
		go func(ing networkingv1.Ingress) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			newName := strings.TrimSuffix(ing.Name, config.NameSuffix)
			log.Printf("Renaming '%s' -> '%s'...", ing.Name, newName)

			if config.DryRun {
				log.Printf("[DRY RUN] Would rename ingress '%s' to '%s'", ing.Name, newName)
				atomic.AddInt32(&renamedCount, 1)
				return
			}

			// Create new ingress without suffix
			newIngress := createNewIngress(&ing, newName, config)
			// Remove the tracking annotations added by copy
			delete(newIngress.Annotations, "copied-from")
			delete(newIngress.Annotations, "copied-by")

			_, err := clientset.NetworkingV1().Ingresses(config.Namespace).Create(ctx, newIngress, metav1.CreateOptions{})
			if err != nil {
				if strings.Contains(err.Error(), "already exists") {
					log.Printf("Warning: Ingress '%s' already exists, deleting suffixed version '%s'", newName, ing.Name)
					// If the final name already exists, just delete the suffixed one
				} else {
					log.Printf("ERROR: Failed to create renamed ingress '%s': %v", newName, err)
					atomic.AddInt32(&failedCount, 1)
					errChan <- fmt.Errorf("failed to rename ingress '%s': %w", ing.Name, err)
					return
				}
			} else {
				log.Printf("Successfully created renamed ingress '%s'", newName)
			}

			// Delete the old suffixed ingress
			err = clientset.NetworkingV1().Ingresses(config.Namespace).Delete(ctx, ing.Name, metav1.DeleteOptions{})
			if err != nil {
				log.Printf("ERROR: Failed to delete suffixed ingress '%s': %v", ing.Name, err)
				atomic.AddInt32(&failedCount, 1)
				errChan <- fmt.Errorf("failed to delete suffixed ingress '%s': %w", ing.Name, err)
			} else {
				log.Printf("Successfully deleted suffixed ingress '%s'", ing.Name)
				atomic.AddInt32(&renamedCount, 1)
			}
		}(ingress)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errChan)

	// Check for errors
	if failedCount > 0 {
		var firstErr error
		for err := range errChan {
			if firstErr == nil {
				firstErr = err
			}
		}
		log.Printf("Summary: %d deleted, %d renamed, %d failed", deletedCount, renamedCount, failedCount)
		return fmt.Errorf("failed to cleanup %d ingresses (first error: %w)", failedCount, firstErr)
	}

	log.Printf("Summary: %d deleted, %d renamed", deletedCount, renamedCount)
	return nil
}

func createNewIngress(original *networkingv1.Ingress, newName string, config Config) *networkingv1.Ingress {
	// Copy labels
	labels := make(map[string]string)
	for k, v := range original.Labels {
		labels[k] = v
	}

	// Copy annotations and add tracking annotations
	annotations := make(map[string]string)
	for k, v := range original.Annotations {
		annotations[k] = v
	}
	annotations["copied-from"] = original.Name
	annotations["copied-by"] = "ingress-migration-tool"

	newIngress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        newName,
			Namespace:   original.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &config.NewIngressClass,
			DefaultBackend:   original.Spec.DefaultBackend,
			TLS:              original.Spec.TLS,
			Rules:            original.Spec.Rules,
		},
	}

	return newIngress
}
