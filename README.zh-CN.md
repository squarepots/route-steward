# Route Steward

[English](README.md) · [简体中文](README.zh-CN.md) · [日本語](README.ja.md) · [Español](README.es.md) · [Português (Brasil)](README.pt-BR.md)

[快速开始](docs/QUICKSTART.md) · [常见问题](docs/FAQ.md) · [兼容性](docs/COMPATIBILITY.md) · [安全](SECURITY.md) · [下载](https://github.com/squarepots/route-steward/releases)

**让 AI 在你控制的服务器上搭建和维护私有代理线路。**

告诉 AI 你有哪些服务器、希望怎样使用这些线路，以及你使用哪些客户端。Route Steward 负责部署受支持的代理路径、验证真实流量，并生成 Clash Verge、Shadowrocket 等客户端可用的私有配置。

## 把仓库交给 AI

```text
打开 https://github.com/squarepots/route-steward，并使用仓库里的 Route Steward skill 帮我在自己控制的服务器上搭建或管理私有代理。
```

安装和第一次使用见[快速开始](docs/QUICKSTART.md)。

## 它负责什么

- Hysteria2 直连线路和两台服务器组成的 WireGuard 中继；
- 线路健康检查、配置偏差检查和可恢复的服务器替换；
- 为受支持的桌面、手机和无界面客户端生成私有配置；
- 为 Mihomo/Clash Verge 兼容客户端和 Shadowrocket 提供可选的私人订阅。

当前支持的主机、客户端、协议和交付方式见[兼容性](docs/COMPATIBILITY.md)。具体主机改动、迁移和恢复流程见[操作说明](OPERATIONS.md)。

运行状态、凭据、生成的客户端配置和恢复材料保存在你选择的私有目录中。云端 AI 仍可能处理执行操作所需的输入。详情见[隐私说明](docs/PRIVACY.md)和[安全说明](SECURITY.md)。

仅使用你拥有或获授权管理的服务器、账户和网络资源。见[运行边界](docs/OPERATING-BOUNDARY.md)。

Route Steward 使用 [AGPL-3.0-only](LICENSE) 许可证。第三方声明见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) 和 [client/vendor/NOTICE.md](client/vendor/NOTICE.md)。
