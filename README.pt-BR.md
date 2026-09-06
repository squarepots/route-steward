# Route Steward

[English](README.md) · [简体中文](README.zh-CN.md) · [日本語](README.ja.md) · [Español](README.es.md) · [Português (Brasil)](README.pt-BR.md)

[Quickstart](docs/QUICKSTART.md) · [FAQ](docs/FAQ.md) · [Compatibility](docs/COMPATIBILITY.md) · [Security](SECURITY.md) · [Releases](https://github.com/squarepots/route-steward/releases)

**Configure e mantenha rotas de proxy privadas em servidores que você controla com um agente de IA.**

Informe ao agente quais servidores você tem, como pretende usar as rotas e quais clientes utiliza. O Route Steward implanta a rota compatível, verifica tráfego real e gera configuração privada para clientes como Clash Verge e Shadowrocket.

## Entregue o repositório ao agente

```text
Open https://github.com/squarepots/route-steward and use its Route Steward skill to set up or manage a private proxy on servers I control.
```

Veja o [Quickstart](docs/QUICKSTART.md) para instalação e primeiro uso.

## O que ele gerencia

- rotas Hysteria2 diretas e relés WireGuard com dois servidores;
- verificação de saúde, inspeção de desvios e substituição retomável de servidores;
- configuração privada para clientes compatíveis de desktop, celular e uso sem interface;
- entrega opcional por assinatura privada para clientes compatíveis com Mihomo/Clash Verge e Shadowrocket.

O suporte atual de hosts, clientes, protocolos e formas de entrega está em [Compatibility](docs/COMPATIBILITY.md). Alterações no host, migração e recuperação estão em [Operations](OPERATIONS.md).

Estado privado, credenciais e configurações geradas ficam no diretório privado escolhido. Um ambiente de IA em nuvem pode processar entradas necessárias para uma operação. Veja [Privacy](docs/PRIVACY.md) e [Security](SECURITY.md).

Route Steward usa a licença [AGPL-3.0-only](LICENSE).
