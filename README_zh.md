<a href="https://kapacity.netlify.app/zh-cn/">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="logo/logo-with-white-text.png">
    <img alt="logo" src="logo/logo-with-black-text.png" width="400">
  </picture>
</a>

[![Go Reference](https://pkg.go.dev/badge/github.com/traas-stack/kapacity.svg)](https://pkg.go.dev/github.com/traas-stack/kapacity)
[![License](https://img.shields.io/github/license/traas-stack/kapacity)](https://www.apache.org/licenses/LICENSE-2.0.html)
![GoVersion](https://img.shields.io/github/go-mod/go-version/traas-stack/kapacity)
[![Go Report Card](https://goreportcard.com/badge/github.com/traas-stack/kapacity)](https://goreportcard.com/report/github.com/traas-stack/kapacity)

> [English](README.md) | 中文

---

♻️ Kapacity 旨在为用户提供一套具备完善技术风险能力的、智能且开放的云原生容量技术，帮助用户安全稳定地实现极致降本增效，解决容量相关问题。

Kapacity 基于蚂蚁集团内部容量系统的核心理念和多年的大规模生产实践经验而构建，该内部容量系统目前已能安全稳定地为蚂蚁持续节省年均约 10w 核的算力成本，同时，Kapacity 也结合了来自云原生社区的最佳实践。

✨ _观看我们在 KubeCon China 2023 上的中文演讲「[我们如何构建生产级 HPA：从智能算法到无风险自动扩缩容](https://mp.weixin.qq.com/s/TKWZhOZxAhB8HiwB2jAuvg)」来深入了解 Kapacity Intelligent HPA 的设计思想和实现原理！_

🚀 _Kapacity 目前仍处于快速迭代的阶段，部分规划中的功能还未完全实现。如果你有任何需求或疑问，欢迎通过[社区](#社区与支持)和我们直接沟通。_

---

## 核心能力

### Intelligent HPA

[Kubernetes HPA](https://kubernetes.io/zh-cn/docs/tasks/run-application/horizontal-pod-autoscale/) 是一项用于对云原生工作负载进行自动扩缩容的常见技术，但它在实际大规模生产使用上的效果和实用度上却并不理想，主要由以下几个原因导致：

* HPA 的自动扩缩容通过响应式的方式驱动，仅当应用负载已经超出设定水位时才会触发扩容，此时容量风险已经出现，只能起到应急的作用而非提前规避风险，尤其对于自身启动时间较长的应用，几乎起不到快速应对流量洪峰的作用。
* HPA 通过简单的指标折比来计算扩缩容目标副本数，只适用于应用副本数和相关指标呈严格线性相关的理想场景，但实际生产当中应用的各类指标和副本数之间存在错综复杂的关系，该算法很难得到符合容量水位要求的副本数。
* 容量弹性作为变更故障率较高的一类场景，HPA 除了支持限制扩缩容速率外没有提供任何其他的风险防控手段，在稳定性要求较高的生产环境中大规模落地是很难令人放心的。
* HPA 作为 Kubernetes 内置能力，一方面自然有其开箱即用的好处，但另一方面也使其绑定在了具体的 K8s 版本上，自身的行为难以被用户扩展或调整，难以满足各类用户在不同应用场景下的定制化需求。

为此，我们构建了 Intelligent HPA (IHPA)，它是一个更加智能化的、具备完善技术风险能力的且高度可扩展定制的 HPA 替代方案。它具有如下几个核心特性：

* **通过多种可按需组合或定制的智能算法来驱动自动扩缩容**
  * 预测式算法：通过多指标时序预测，并结合对组分流量和应用容量及其对应副本数的综合建模，推理得出应用未来的推荐副本数。该算法能够很好地应对生产上多周期流量、趋势变化流量、多条流量共同影响容量、容量与副本数呈非线性关系等复杂场景，通用性和准确性兼具。
  * 突增式算法：通过对近一段时间的指标进行持续监控分析，快速发现潜在的流量异常或容量恶化情况，在容量风险实际发生前主动响应，及时进行扩容等操作以规避风险。
  * 此外，也内置了传统的响应式折比算法和基于定时规则进行扩缩容的能力。
* **具备多种变更风险防控能力**
  * 支持在整个弹性过程中精细化地控制工作负载下每一个 Pod 的状态，比如仅摘除 Pod 流量或释放 Pod 计算资源但不实际终止或删除 Pod 等，实现多阶段缩容。通过这种灵活的 Pod 状态转换能够显著提升弹性效率并降低弹性风险。
  * 执行扩缩容时支持采用灰自定义度分批的变更策略，最大程度地减小了弹性变更的爆炸半径；同时还支持和上面的精细化 Pod 状态控制能力相结合来实现多阶段灰度，提升应急回滚速度，进一步降低弹性变更风险。
  * 支持用户自定义的变更期稳定性检查，包括自定义指标（不局限于用于弹性伸缩的指标）异常判断等，多维度地分析变更状况，一旦发现异常支持自动采取应急熔断措施，如变更暂停或变更回滚，真正做到弹性变更常态化无人值守。
* **开放可扩展的架构**
  * 整个 IHPA 能力拆分为了管控、决策、执行三大模块，任一模块都可以做替换或扩展。
  * 提供了大量扩展点，使得其行为能够被用户自由扩展或调整。可扩展的部分包括但不限于自定义 Pod 摘挂流的逻辑、自定义 Pod 缩容优先级、自定义变更期稳定性检查逻辑等。

## 开始使用 Kapacity

访问 [kapacity.netlify.app](https://kapacity.netlify.app/zh-cn/) 来查阅官方文档。

或者跟随[快速开始教程](https://kapacity.netlify.app/zh-cn/docs/getting-started/)来快速入门。

## 社区与支持

如果你有任何问题或想法，可以通过下面的途径得到帮助与反馈：

* 想询问一些通用的或使用上的问题，或者有任何想法？→ [GitHub Discussions](https://github.com/traas-stack/kapacity/discussions)
* 想上报一个 BUG 或提交一个需求？→ [GitHub Issues](https://github.com/traas-stack/kapacity/issues)
* 希望更深入地交流？欢迎通过下面的任意渠道加入我们的社区讨论：
  * [钉钉](https://qr.dingtalk.com/action/joingroup?code=v1,k1,7qkY1oyphgJvdUE4nJ1EcnNvE2JhmoNXBgdVTvD3AX0=&_dt_no_comment=1&origin=11)（主要通过中文来交流，群号 27855025593）
  * [Slack](https://join.slack.com/t/traas-kapacity/shared_invite/zt-1w1esmmk5-bNy3~IuGeCWQ21UmCexcrA)（主要通过英文来交流）

## 贡献

我们热忱地欢迎任何形式的贡献 🤗，你可以阅读[贡献指南](https://kapacity.netlify.app/zh-cn/docs/contribution-guidelines/)来了解更多贡献相关信息。
