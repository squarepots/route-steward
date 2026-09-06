# Route Steward

[English](README.md) · [简体中文](README.zh-CN.md) · [日本語](README.ja.md) · [Español](README.es.md) · [Português (Brasil)](README.pt-BR.md)

[Quickstart](docs/QUICKSTART.md) · [FAQ](docs/FAQ.md) · [Compatibility](docs/COMPATIBILITY.md) · [Security](SECURITY.md) · [Releases](https://github.com/squarepots/route-steward/releases)

**Configura y mantiene rutas de proxy privadas en servidores que controlas con un agente de IA.**

Indica al agente qué servidores tienes, cómo quieres usar las rutas y qué clientes utilizas. Route Steward despliega la ruta compatible, verifica tráfico real y genera configuración privada para clientes como Clash Verge y Shadowrocket.

## Dáselo a un agente

```text
Open https://github.com/squarepots/route-steward and use its Route Steward skill to set up or manage a private proxy on servers I control.
```

Consulta [Quickstart](docs/QUICKSTART.md) para la instalación y el primer uso.

## Qué gestiona

- rutas Hysteria2 directas y relés WireGuard de dos servidores;
- comprobación de salud, inspección de cambios y reemplazo reanudable de servidores;
- configuración privada para clientes de escritorio, móviles y sin interfaz compatibles;
- entrega opcional mediante suscripción privada para clientes Mihomo/Clash Verge compatibles y Shadowrocket.

El soporte actual de hosts, clientes, protocolos y entrega está en [Compatibility](docs/COMPATIBILITY.md). Los efectos sobre el host, migración y recuperación están en [Operations](OPERATIONS.md).

El estado privado, credenciales y configuraciones generadas permanecen en el directorio privado que elijas. Un entorno de IA en la nube puede procesar entradas necesarias para una operación. Consulta [Privacy](docs/PRIVACY.md) y [Security](SECURITY.md).

Route Steward usa la licencia [AGPL-3.0-only](LICENSE).
