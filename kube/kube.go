package kube

import (
	"fmt"
	"sort"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batch "k8s.io/api/batch/v1"
	"k8s.io/api/core/v1"

	"github.com/sstarcher/job-reaper/alert"
)

// Client Interface for reaping
type Client interface {
	Reap()
}

type kubeClient struct {
	clientset             *kubernetes.Clientset
	maxFailures           int
	keepCompletedDuration time.Duration
	alerters              *[]alert.Alert
	numReapers            int
	bufferDepth           int
	ignoreOwned           bool
}

type byCompletion []batch.Job

func (bc byCompletion) Len() int {
	return len(bc)
}

func (bc byCompletion) Less(i, j int) bool {
	if bc[i].Status.CompletionTime == nil {
		return false
	}

	if bc[j].Status.CompletionTime == nil {
		return true
	}
	return bc[i].Status.CompletionTime.Before(bc[j].Status.CompletionTime)
}

func (bc byCompletion) Swap(i, j int) {
	bc[i], bc[j] = bc[j], bc[i]
}

// NewKubeClient for interfacing with kubernetes
func NewKubeClient(masterURL string, maxFailures int,
	keepCompletedDuration time.Duration,
	ignoreOwned bool,
	alerters *[]alert.Alert, reaperCount, bufferDepth int) Client {
	config, err := clientcmd.BuildConfigFromFlags(masterURL, "")
	if err != nil {
		log.Panic(err.Error())
	}
	config.QPS = float32(3 * reaperCount)
	config.Burst = int(2 * config.QPS)
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Panic(err.Error())
	}

	return &kubeClient{
		clientset:             clientset,
		maxFailures:           maxFailures,
		keepCompletedDuration: keepCompletedDuration,
		alerters:              alerters,
		numReapers:            reaperCount,
		bufferDepth:           bufferDepth,
		ignoreOwned:           ignoreOwned,
	}
}

func (kube *kubeClient) reap(job batch.Job) {
	data := alert.Data{
		Name:      job.GetName(),
		Namespace: job.GetNamespace(),
		Status:    "Unknown",
		Message:   "",
		Config:    job.GetAnnotations(),
	}

	pods, err := kube.jobPods(job)

	if err != nil {
		if _, ok := err.(*apiErrors.StatusError); ok {
			log.Warnf("Could not fetch jobPods. Skipping for now. Error: %v", err)
			return
		}
		log.Panic(err.Error())
	}
	pod := kube.oldestPod(pods)

	if scheduledJobName, ok := pod.GetLabels()["run"]; ok {
		data.Name = scheduledJobName
	}

	if pod.Status.Phase != "" {
		data.Status = string(pod.Status.Phase)
	}

	if len(pod.Status.ContainerStatuses) > 0 { //Container has exited
		terminated := pod.Status.ContainerStatuses[0].State.Terminated
		if terminated != nil {
			data.Message = terminated.Reason // ERRRRR
			data.ExitCode = int(terminated.ExitCode)
			data.StartTime = terminated.StartedAt.Time
			data.EndTime = terminated.FinishedAt.Time
		} else {
			log.Error("Unexpected null for container state")
			log.Error(pod.Status.ContainerStatuses[0])
			log.Error(terminated)
			log.Error(job)
			log.Error(job.Status.Conditions)
			log.Error(pod)
			log.Error(kube.podEvents(pod))
			return
		}
	} else if len(job.Status.Conditions) > 0 { //TODO naive when more than one condition
		condition := job.Status.Conditions[0]
		data.Message = fmt.Sprintf("Pod Missing: %s - %s", condition.Reason, condition.Message)
		if condition.Type == batch.JobComplete {
			data.ExitCode = 0
			data.Status = "Succeeded"
		} else {
			data.ExitCode = 998
		}
		data.StartTime = job.Status.StartTime.Time
		data.EndTime = condition.LastTransitionTime.Time
	} else { //Unfinished Containers or missing
		data.ExitCode = 999
		data.EndTime = time.Now()
	}

	for _, alert := range *kube.alerters {
		err := alert.Send(data)
		if err != nil {
			log.Error(err.Error())
		}
	}

	go func() {
		err := kube.clientset.Batch().Jobs(data.Namespace).Delete(job.GetName(), nil)
		if err != nil {
			log.Error(err.Error())
		}

		log.Debugln("Deleting pods for ", data.Name)
		for _, pod := range pods.Items {
			err := kube.clientset.Core().Pods(data.Namespace).Delete(pod.GetName(), nil)
			if err != nil {
				log.Error(err.Error())
			}
		}
		log.Debugln("Done deleting pods for ", data.Name)
	}()
}

func (kube *kubeClient) jobPods(job batch.Job) (*v1.PodList, error) {
	controllerUID := job.Spec.Selector.MatchLabels["controller-uid"]
	selector := labels.NewSelector()
	requirement, err := labels.NewRequirement("controller-uid", selection.Equals, sets.NewString(controllerUID).List())
	if err != nil {
		log.Panic(err.Error())
	}
	selector = selector.Add(*requirement)
	pods, err := kube.clientset.Core().Pods(job.ObjectMeta.Namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		log.Error(err.Error())
	}

	return pods, err
}

func (kube *kubeClient) podEvents(pod v1.Pod) *v1.EventList {
	sel, err := fields.ParseSelector("involvedObject.name=" + pod.ObjectMeta.Name)
	if err != nil {
		log.Panic(err.Error())
	}
	events, err := kube.clientset.Core().Events(pod.ObjectMeta.Namespace).List(metav1.ListOptions{
		FieldSelector: sel.String(),
	})
	return events
}

func (kube *kubeClient) oldestPod(pods *v1.PodList) v1.Pod {
	time := time.Now()
	var tempPod v1.Pod
	for _, pod := range pods.Items {
		if time.After(pod.ObjectMeta.CreationTimestamp.Time) {
			time = pod.ObjectMeta.CreationTimestamp.Time
			tempPod = pod
		}
	}
	return tempPod
}

func reaper(kube *kubeClient, jobs <-chan batch.Job, done <-chan struct{}) {
	for job := range jobs {
		kube.reap(job)

		select {
		case <-done:
			return
		default:
			//Noop
		}
	}
}

func (kube *kubeClient) Reap() {
	namespaces, err := kube.clientset.Core().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		log.Panic(err.Error())
	}

	var wg sync.WaitGroup
	wg.Add(kube.numReapers)
	bufferSize := kube.numReapers * kube.bufferDepth
	jobs := make(chan batch.Job, bufferSize)
	done := make(chan struct{})
	defer close(done)

	log.Infof("Spawning %d reapers with buffer depth of %d", kube.numReapers, bufferSize)
	for i := 0; i < kube.numReapers; i++ {
		go func() {
			reaper(kube, jobs, done)
			wg.Done()
		}()
	}

	for _, namespace := range namespaces.Items {
		log.Debugf("Processing namespace: %s", namespace.ObjectMeta.Name)
		kube.reapNamespace(namespace.ObjectMeta.Name, jobs)
	}
	close(jobs)
	wg.Wait()
}

func (kube *kubeClient) reapNamespace(namespace string, jobQueue chan<- batch.Job) {
	jobs, err := kube.clientset.Batch().Jobs(namespace).List(metav1.ListOptions{})
	if err != nil {
		log.Panic(err.Error())
	}

	sort.Sort(byCompletion(jobs.Items))

	for _, job := range jobs.Items {
		if kube.shouldReap(job) {
			jobQueue <- job
		}
	}
}

func (kube *kubeClient) shouldReap(job batch.Job) bool {
	if kube.ignoreOwned && len(job.ObjectMeta.OwnerReferences) > 0 {
		return false
	}

	// Always reap if number of failures has exceed maximum
	if kube.maxFailures >= 0 && int(job.Status.Failed) > kube.maxFailures {
		return true
	}

	// Don't reap anything that hasn't met its completion count
	if int(job.Status.Succeeded) < getJobCompletions(job) {
		return false
	}

	// Don't reap completed jobs that aren't old enough
	if job.Status.CompletionTime != nil && time.Since(job.Status.CompletionTime.Time) < kube.keepCompletedDuration {
		return false
	}

	return true
}

func getJobCompletions(job batch.Job) int {
	if job.Spec.Completions != nil {
		return int(*job.Spec.Completions)
	}
	return 1
}
