// JSON Schema for `.luuna/missions/config.yaml`. Wire-format contract for
// schema-driven config UIs (such as the Luuna desktop): a UI reads this schema,
// renders a form, and writes back via `luuna-missions config set <path> <value>`. Locked
// with a golden test (config.test.ts) so any shape change is intentional.
//
// Mirrors the ground-truth shape produced by readConfig + loadPlanTemplates in
// src/config.ts. Built-in plan templates appear in `examples` so a UI
// can offer a "copy a built-in to start" affordance.

import {
	BUILTIN_BUG_TEMPLATE,
	BUILTIN_FEATURE_TEMPLATE,
	BUILTIN_REFACTOR_TEMPLATE,
} from "./config.ts";
import { DEFAULT_MAX_PLAN_DEPTH, type PlanTemplate } from "./types.ts";

type JSONSchema = Record<string, unknown>;

function templateExample(t: PlanTemplate): Record<string, unknown> {
	const out: Record<string, unknown> = { sections: t.sections };
	if (t.description) out.description = t.description;
	return out;
}

export function configSchema(): JSONSchema {
	return {
		$schema: "https://json-schema.org/draft/2020-12/schema",
		$id: "https://github.com/luuna-ai/luuna-missions/config.schema.json",
		title: "Missions project config",
		description:
			"Schema for .luuna/missions/config.yaml. Consumed by schema-driven configuration UIs; emit via `luuna-missions config schema`.",
		type: "object",
		required: ["project", "version"],
		additionalProperties: false,
		properties: {
			project: {
				type: "string",
				minLength: 1,
				title: "Project name",
				description: "Used as the prefix for issue IDs (e.g. `<project>-a1b2`).",
			},
			version: {
				type: "string",
				title: "Config schema version",
				description: "Internal version tag for the config layout. Bumped when the schema breaks.",
				default: "1",
			},
			max_plan_depth: {
				type: "integer",
				minimum: 1,
				title: "Max plan depth",
				description:
					"Display-only depth limit for `luuna-missions plan show` recursion through nested sub-plans.",
				default: DEFAULT_MAX_PLAN_DEPTH,
			},
			plan_templates: {
				type: "object",
				title: "Custom plan templates",
				description:
					"Map of template name → template definition. Overrides the built-in `feature`, `bug`, and `refactor` templates when names collide.",
				additionalProperties: { $ref: "#/$defs/PlanTemplate" },
				examples: [
					{
						feature: templateExample(BUILTIN_FEATURE_TEMPLATE),
						bug: templateExample(BUILTIN_BUG_TEMPLATE),
						refactor: templateExample(BUILTIN_REFACTOR_TEMPLATE),
					},
				],
			},
		},
		$defs: {
			PlanTemplate: {
				type: "object",
				required: ["sections"],
				additionalProperties: false,
				properties: {
					description: {
						type: "string",
						title: "Description",
						description: "Human-readable summary shown in `luuna-missions plan templates`.",
					},
					sections: {
						type: "object",
						title: "Sections",
						description:
							"Map of section name → spec. Each section becomes a field in the plan submission.",
						additionalProperties: { $ref: "#/$defs/SectionSpec" },
					},
				},
			},
			SectionSpec: {
				type: "object",
				required: ["required", "kind", "prompt"],
				additionalProperties: false,
				properties: {
					required: {
						type: "boolean",
						title: "Required",
						description: "Whether this section must be present in the plan submission.",
					},
					kind: {
						title: "Kind",
						description:
							"`text` for free text, `list` for an array, `steps` for an ordered step list, or an object describing nested sub-fields.",
						oneOf: [
							{ type: "string", enum: ["text", "list", "steps"] },
							{
								type: "object",
								additionalProperties: { $ref: "#/$defs/SectionSpec" },
							},
						],
					},
					prompt: {
						type: "string",
						title: "Prompt",
						description: "Question or instruction shown to the LLM when filling this section.",
					},
					min_length: {
						type: "integer",
						minimum: 0,
						title: "Minimum length",
						description: "Minimum character count when `kind` is `text`.",
					},
					min: {
						type: "integer",
						minimum: 0,
						title: "Minimum items",
						description: "Minimum item count when `kind` is `list` or `steps`.",
					},
					item: {
						title: "Item shape",
						description:
							"When `kind` is `list`, the per-item shape: `text` for strings or an object spec for structured items.",
						oneOf: [
							{ type: "string", const: "text" },
							{
								type: "object",
								additionalProperties: { $ref: "#/$defs/SectionSpec" },
							},
						],
					},
					atlas_source: {
						type: "string",
						title: "Atlas source",
						description:
							"Optional record type (e.g. `failure`, `decision`) seeded into `prior_art` when emitting a plan prompt.",
					},
				},
			},
		},
	};
}
