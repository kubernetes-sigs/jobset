# JobSet

JobSet: An API for managing a group of Jobs as a unit.

# Installation

To install the CRD and deploy the controller on the cluster selected on your `~/.kubeconfig`, run the following commands:
```
git clone https://github.com/kubernetes-sigs/jobset.git
cd jobset
IMAGE_REGISTRY=<registry>/<project> make image-push deploy
```

## Community, discussion, contribution, and support

Learn how to engage with the Kubernetes community on the [community page](http://kubernetes.io/community/).

You can reach the maintainers of this project at:

- [Slack](https://kubernetes.slack.com/messages/sig-apps)
- [Mailing List](https://groups.google.com/forum/#!forum/kubernetes-sig-apps)

### Code of conduct

Participation in the Kubernetes community is governed by the [Kubernetes Code of Conduct](code-of-conduct.md).
