# ⚡ Actionforge Graph Runner
<!-- markdownlint-disable MD033 -->

<div align="center" width="100%">

  <img src="assets/logo.svg" alt="Graph Runner Logo">

[![view-action-graph](https://img.shields.io/github/actions/workflow/status/actionforge/actrun-cli/workflow.yml?label=View%20Action%20Graph)](https://app.actionforge.dev/github/actionforge/actrun-cli/main/.github/workflows/graphs/build-test-publish.act)
[![made-with-go](https://img.shields.io/badge/Made%20with-Go-86D4DE.svg)](https://go.dev)
[![License](https://img.shields.io/badge/License-ACT-blue?color=orange)](https://www.github.com/actionforge/legal/blob/main/LICENSE.md)

</div>

`actrun` is the CLI execution engine for [Actionforge](https://www.actionforge.dev). It is a binary designed to execute `.act` graph files natively on your machine, CI runners, or render nodes.

It handles the traversal, concurrency, and data flow of visual graphs created in the Actionforge web app. It supports GitHub Actions, GitHub Actions workflows and custom 3D/CG/VFX pipelines.

## 🏁 Getting Started

### 📥 Installation

Requires **Go 1.25+**.

```bash
git clone [https://github.com/actionforge/actrun-cli](https://github.com/actionforge/actrun-cli)
cd actrun-cli
go mod tidy
go run . # to run without building
go build -o actrun . # to build the binary

```

### ✏️ Editor Setup

While `actrun` handles execution, visit [app.actionforge.dev](https://app.actionforge.dev) to build and edit `.act` files.

## 🚀 Usage

The basic syntax is `actrun [filename] [flags]`.

### ▶️ 1. Execute a Graph

Run a graph file directly. The CLI will load the graph, resolve dependencies, and execute the node chain.

```bash
./actrun ./my_graph.act


```

### 🔌 2. Pass Arguments

You can pass arbitrary arguments to the graph. `actrun` interprets arguments following the graph filename as inputs to the execution context.

```bash
# Pass inputs to the graph logic
actrun ./my_graph.act --target="production" --verbose


```

If you need to strictly separate CLI flags from graph arguments, use the `--` separator:

```bash
actrun --env_file=.env -- ./my_graph.act --target="production"


```

### 🌍 3. Load Environment Variables

Inject environment variables from a file before execution starts using `--env_file`.

```bash
actrun --env_file=.env.local ./my_graph.act


```

### 🛡️ 4. Validation

Before running a graph, you can check the graph for structural errors, disconnected pins, type mismatches, or missing required inputs without executing it.

```bash
actrun validate ./complex_workflow.act


```

## 🔮 Advanced Features

### 🕸️ Debug Sessions

`actrun` can bridge your local terminal to the Actionforge web app for visual debugging. You can either connect to your browser session via a debug session token that your browser provided, or you can let the CLI intiate a debug session by using `--create_debug_session`. The latter will print a link to stdout that you can open in your browser and the debug session will immediately begin.

```bash
actrun --create_debug_session ./my_graph.act


```

### 🚦 Concurrency Control

By default, concurrency is enabled but you can disable it using the `--concurrency` flag. It will force all "Concurrent" nodes to run in serial instead.

```bash
# Disable concurrency for strict serial execution
actrun --concurrency=false ./sequential_task.act


```

## 🛠️ Development Commands

If you are contributing to the core nodes or the CLI itself, the `dev` subcommand provides utilities to maintain the internal registry.

* **Generate Stubs**: Rebuilds the Go interfaces for nodes based on embedded YAML definitions.

```bash
go run -tags=dev,api,cpython . dev generate_stubs

```

## ⚖️ Full License Text

For the complete legal documentation and full terms of service, please refer to:

* 📄 **Source Code:** [actionforge/legal/LICENSE.md](https://github.com/actionforge/legal/blob/main/LICENSE.md)
* 🌐 **Official Website:** [actionforge.dev/eula](https://www.actionforge.dev/eula)
