---
date: 2026-04-24
title: Bedrock via AWS CLI
status: draft
---

# Bedrock via AWS CLI

## 1) Summary

This branch implements Bedrock via AWS CLI.

User-facing name:

- Bedrock via AWS CLI

Implementation-facing name:

- AWS CLI-backed Bedrock provider

The provider continues to use `llm.provider=bedrock`, but the backend transport is now the local AWS CLI command:

- `aws bedrock-runtime converse`

## 2) Why This Exists

The primary goal is to support local AWS credential resolution that depends on the operator environment, especially:

- AWS profiles such as `llm.bedrock.aws_profile`
- existing local AWS CLI configuration
- local AWS auth flows that are already working outside the app

This makes the Bedrock path usable in environments where static `aws_key` / `aws_secret` are not the preferred integration mode.

## 3) What The Provider Supports

The AWS CLI-backed Bedrock provider supports the existing `llm.Request` contract used by the agent runtime:

- text chat
- system messages
- tool calls
- tool results
- multimodal image parts
- standard inference parameters such as `temperature`, `top_p`, `max_tokens`, and `stop`

It translates the internal request into Bedrock `converse` payloads and parses Bedrock `toolUse` / `toolResult` responses back into the shared `llm.Result` shape.

## 4) Config Surface

The config namespace stays the same:

- `llm.provider=bedrock`
- `llm.bedrock.aws_key`
- `llm.bedrock.aws_secret`
- `llm.bedrock.aws_profile`
- `llm.bedrock.region`
- `llm.bedrock.model_arn`

Credential sources:

- explicit AWS key / secret
- AWS profile via `llm.bedrock.aws_profile`

The latest follow-up fix on this branch wires `aws_profile` all the way into the Bedrock client so the spawned AWS CLI process actually uses the configured profile.

Credential precedence:

- if `llm.bedrock.aws_key` / `llm.bedrock.aws_secret` are set, they are passed to the AWS CLI process as explicit environment credentials
- if `llm.bedrock.aws_profile` is set, it is passed as `AWS_PROFILE`
- when both are present, the expected effective behavior is that explicit environment credentials win and the profile acts only as a secondary source/context

This means existing users who already configure Bedrock with explicit credentials should continue to work, while profile-based users gain a supported path without requiring static credentials in config.

## 5) Compatibility And Impact

Impact on other providers:

- none

Impact on the public Bedrock config surface:

- none beyond adding `llm.bedrock.aws_profile`

Impact on existing Bedrock usage in this branch:

- no intended behavioral regression
- the follow-up changes only fix missing config wiring and restore compact chat header behavior

Implementation note for upstream reviewers:

- this is a backend transport change from a uniai-backed Bedrock path to an AWS CLI-backed Bedrock path
- the intent is to preserve the same high-level provider role in the runtime while making local AWS-auth-driven operation practical

## 6) Validation

Validated on this branch with:

- `go test ./internal/llmutil ./cmd/mistermorph/chatcmd ./providers/bedrock`
- `go build -o ./bin/mistermorph ./cmd/mistermorph`
- manual `mistermorph chat --compact-mode` verification with Bedrock config

## 7) Example

```yaml
llm:
  provider: bedrock
  bedrock:
    aws_profile: common-api-dev
    region: ap-northeast-1
    model_arn: anthropic.claude-3-5-sonnet-20240620-v1:0
```

This should be described upstream as:

- Bedrock via AWS CLI for operators
- AWS CLI-backed Bedrock provider in implementation terms
