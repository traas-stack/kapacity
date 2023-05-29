# Kapacity

[![Go Reference](https://pkg.go.dev/badge/github.com/traas-stack/kapacity.svg)](https://pkg.go.dev/github.com/traas-stack/kapacity)
[![License](https://img.shields.io/github/license/traas-stack/kapacity)](https://www.apache.org/licenses/LICENSE-2.0.html)
![GoVersion](https://img.shields.io/github/go-mod/go-version/traas-stack/kapacity)
[![Go Report Card](https://goreportcard.com/badge/github.com/traas-stack/kapacity)](https://goreportcard.com/report/github.com/traas-stack/kapacity)

> English | [中文](README_zh.md)

---

♻️ Kapacity is an open cloud native capacity solution which helps you achieve ultimate resource utilization in an intelligent and risk-free way.

It automates your scaling, mitigates capacity risks, saves your effort as well as cost.

Kapacity is built upon core ideas and years of experience of the large-scale production capacity system at Ant Group, which saves ~100k cores yearly with high stability and zero downtime, combined with best practices from the cloud native community.

🚀 _Please note that Kapacity is still under active development, and not all features proposed have been implemented. Feel free to directly talk to us through [community](#community--support) if you have any wish or doubt._

---

## Core Features

### Intelligent HPA

[Kubernetes HPA](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/) is a common way used to scale cloud native workloads automatically, but it has some BIG limitations listed below which make it less effective and practical in real world large-scale production use:

* HPA works in a reactive way, which means it would only work AFTER the target metrics exceeding the expected value. It can hardly provide rapid and graceful response to traffic peaks, especially for applications with longer startup times.
* HPA calculates replica count based on metrics by a simple ratio algorithm, with an assumption that the replica count must have a strict linear correlation with related metrics. However, this is not always the case in real world.
* Scaling is a highly risky operation in production, but HPA provides little risk mitigation means other than scaling rate control.
* HPA is a Kubernetes built-in, well, this is not a limitation literally, but it does limit some functions/behaviors to specific Kubernetes versions, and there is no way for end users to extend or adjust its functionality for their own needs.

So we build Intelligent HPA (IHPA), an intelligent, risk-defensive, highly adaptive and customizable substitution for HPA. It has below core features:

* **Autoscaling powered by multiple intelligent algorithms, all combinable and customizable**
  * Algorithm which predicts appropriate replica counts in the future, utilizing time series forecasting of metrics and advanced metrics-replicas modeling, which makes it suitable for a variety of scenarios in real world production, such as multi period and trending traffic, load affected by multiple traffics, non-linear correlation between load and replica count, and so on.
  * Algorithm which detects abnormal traffic or potential capacity risks, and suggests a safe replica count proactively.
  * Also, the classic reactive ratio algorithm and cron-based replica control are batteries included.
* **Scaling with multiple risk defense means**
  * Fine-grained pod state control which enables a multi-stage scale down. You can scale down a pod by only turning off its traffics, or releasing its resources without actually stopping the application or deleting the pod. This can greatly increase the speed of rollback (scale up again) if needed.
  * Fully customizable gray change for both scale up and scale down. You can even combine it with the pod state control mechanism to achieve multi-stage gray change.
  * Automatic risk mitigation based on customizable stability checks. You can let it monitor arbitrary metrics (not limited to the metrics which drive autoscaling) for risk detection, or even define your own detection logic, and it can automatically take actions such as suspend or rollback the scaling to mitigate risks.
* **Open and highly extensible architecture**
  * IHPA is split into three independent modules for replica count calculation, workload replicas control and overall autoscaling process management. Each module is replaceable and extensible.
  * Various extension points are exposed which makes the behavior of IHPA fully customizable and extensible. For example, you can customize how to control traffics of the pod, which pods shall be scaled down first, how to detect risks during autoscaling and so on.

## To start using Kapacity

See our documentation on [kapacity.netlify.app](https://kapacity.netlify.app).

Walking through the [Quick Start Tutorial](https://kapacity.netlify.app/docs/getting-started/) is also a good way to get started.

## Community & Support

You've got questions, or have any ideas? Here's the ways:

* Have some general questions or ideas? → [GitHub Discussions](https://github.com/traas-stack/kapacity/discussions)
* Want to report a bug or request a feature? → [GitHub Issues](https://github.com/traas-stack/kapacity/issues)
* Want further more connections? Join our community by:
  * [Slack](https://join.slack.com/t/traas-kapacity/shared_invite/zt-1w1esmmk5-bNy3~IuGeCWQ21UmCexcrA) (for English speakers mainly)
  * [DingTalk](https://qr.dingtalk.com/action/joingroup?code=v1,k1,7qkY1oyphgJvdUE4nJ1EcnNvE2JhmoNXBgdVTvD3AX0=&_dt_no_comment=1&origin=11) (for Chinese speakers mainly, group number is 27855025593)

## Contributing

Any form of contributing is warmly welcomed 🤗, read the [contribution guidelines](https://kapacity.netlify.app/docs/contribution-guidelines/) for details.
