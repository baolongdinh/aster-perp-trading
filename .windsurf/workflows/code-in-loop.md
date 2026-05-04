---
description: This plan implements a Hard-core Autonomous Loop (HAL), shifting from passive "Chat-Wait-Review" interactions to a Self-Operating Engineering System. By leveraging the Model Context Protocol (MCP), the workflow bridges the gap between LLM reasonin
---

auto_execution_mode: 3

🚀 HARD-CORE LOOP: MCP-AUGMENTED EXECUTION PLAN

1. Overview
   This project adopts a Hard-core Autonomous Loop (HAL), transforming development from passive "Chat-Wait-Review" into a Self-Operating Engineering System.

The MCP Advantage: We leverage the full suite of MCP tools (Filesystem, Git, Docker, Shell, and Database) to provide the model with "Real-World Context." This ensures the model doesn't just "guess" code—Natively, it observes, executes, and validates with zero context-leakage.

2. Core Execution Roles (Internal Agents)
   🧠 Planner (The Context Architect)
   MCP Superpower: Uses ls, grep, and read_file to map the entire repository topology before speaking.

Action: \* Map microservice boundaries and entry points (gRPC/Gateway).

Identify "Context Anchors" (Proto files, DB schemas, ENV configs).

Produce a multi-layer implementation plan (Presentation → Logic → Data).

💻 Executor (The MCP Engineer)
MCP Superpower: Uses write_file and edit_file with surgical precision.

Action: \* Implement features across services.

Maintain strict Clean Architecture patterns.

Auto-generate migrations and Proto definitions without being asked.

🔍 Critic (The Security & Logic Reviewer)
MCP Superpower: Uses grep and shell_execute to cross-reference dependencies and check for breaking changes in other services.

Action: \* Must identify at least 3 failure points (e.g., Idempotency, Race Conditions, Transaction safety).

Validate against Context7 (Ensure no stale context or hallucinated variables).

🛠 Debugger (The Automated Operator)
MCP Superpower: Uses shell_execute to run go test, docker-compose up, and tail logs in real-time.

Action: \* Analyze stack traces and logs.

Apply fixes and re-run the loop until Exit Code = 0.

3. The MCP-Driven Autonomous Loop
   The system strictly follows this closed-circuit loop:

Observe (MCP): Scan current state and dependencies.

Plan: Propose architecture.

Execute (MCP): Write/Modify code.

Critique: High-level validation + Risk assessment.

Validate (MCP): Run tests/logs.

Self-Correct: Repeat until stable.

Loop Constraint: No user interruption unless the same error persists for 3 consecutive cycles.

4. Windsurf Rules (.windsurfrules)
   YAML

# MODE: HARD-CORE AUTONOMOUS LOOP (MCP-OPTIMIZED)

- Context-First-Observation:
  Always use MCP 'ls' and 'grep' before proposing any code changes to ensure 100% context accuracy.

- Superpower-Execution:
  Leverage Shell for 'go' commands, migrations, and docker orchestration.
  Never ask for permission to run tests.

- Self-Critique (The Rule of 3):
  Identify 3 risks: Service failure, DB inconsistency, or Race conditions.

- Autonomous-Stability:
  Retries are mandatory. Do NOT stop unless tests pass or 3-retry block occurs.

- Zero-Hallucination:
  If a variable/method is not found via MCP tools, it does not exist. Do not invent it.

5. Hard-core Activation Prompt
   Plaintext
   Activate Hard-core Loop (MCP Mode).

Task: [INSERT TASK]

Execution Requirements:

1. FULL SCAN: Use MCP tools to analyze the current microservice architecture.
2. CONTEXT OPTIMIZATION: Identify relevant Proto, Domain, and Infra files.
3. EXECUTE: Implement changes across all services.
4. VALIDATE: Run 'go test ./...' and check service logs via Shell.

Stopping Conditions:

- All integration tests pass.
- System identifies a hard block (e.g., missing credentials).

START NOW. 6. Context Optimization & "Context7" Strategy
To ensure the model operates at peak efficiency without "Context Overflow":

Surgical Loading: Use MCP to read only the interfaces and dependency trees, not the entire implementation of every file.

Bridge-Awareness: Focus context on Contracts (Proto files, Message schemas) when working on inter-service communication.

Context Clearing: After a successful loop, summarize the "New System State" and clear temporary execution logs to maintain a clean "Context7" window.

7. Expected Outcomes
   🔄 Self-Healing: The system detects its own gRPC/Database errors and fixes them.

🧩 Deep Context: MCP ensures the model knows the exact state of the PostgreSQL schema or RabbitMQ queues.

⚡ Velocity: Drastic reduction in manual "copy-pasting" of errors back to the AI.
