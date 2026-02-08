# JobSet API

Python API models for the [JobSet](https://github.com/kubernetes-sigs/jobset) Kubernetes API,
auto-generated from the OpenAPI specification.

## Installation

```bash
pip install jobset_api
```

## Usage

```python
from jobset_api.models import JobsetV1alpha2JobSet

job_set = JobsetV1alpha2JobSet(
    metadata={"name": "example"},
)
```

## License

Apache License 2.0
