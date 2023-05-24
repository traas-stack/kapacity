# Contributing to Kapacity

Thank you so much for helping kapacity become better. You can contribute to kapacity in the following ways:

* [Submit issues](#submit-issues)
* [Contributing Changes](#contributing-changes)

## Submit issues

Before submitting an issue, please check whether there is a similar issue in
[existing issues](https://github.com/traas-stack/kapacity/issues).

- Submit bug report according to
  the [template](https://github.com/traas-stack/kapacity/issues/new?assignees=&labels=kind%2Fbug&projects=&template=bug-report.yaml),
  and the description should be as detailed as possible to facilitate us to reproduce the problem.
- If you have an idea to improve Kapacity, submit
  an [feature request](https://github.com/traas-stack/kapacity/issues/new?assignees=&labels=kind%2Ffeature&projects=&template=feature-request.yaml).

## Contributing Changes

Kapacity is a project developed based on golang, Code submission please follow
the [Go Style Guide](https://google.github.io/styleguide/go/decisions).

- All code files submitted must contain copyright information, the copyright information comment block needs to
  be included at the beginning.
- When referencing the k8s API type (including CRD) in the code, the group name must be included in the import alias,
  such as autoscalingv1alpha1, corev1, etc. Such as v1alpha1, v1 cannot be used directly to avoid possible future
  reference conflicts and improve code readability. In particular, when the CRD group name conflicts with the k8s
  built-in group name (autoscaling group in this project), it is necessary to add the k8s prefix before the k8s built-in
  group name, such as k8sautoscalingv2.
- The commit message needs to be able to describe the specific content of the commit modification, not just fix, update,
  etc.

