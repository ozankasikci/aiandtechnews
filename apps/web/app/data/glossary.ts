// Approved glossary terms (reviewed 2026-09-26). Articles underline the first
// mention of a term or alias; /glossary/[slug] has a page for each. Definitions
// follow the writing contract: plain sentences, no em dash.

export interface GlossaryTerm {
  slug: string;
  term: string;
  aliases: string[];
  definition: string;
}

export const GLOSSARY: GlossaryTerm[] = [
  {
    slug: "agentic-ai",
    term: "Agentic AI",
    aliases: ["agentic"],
    definition: "AI built to plan and take several steps toward a goal with little human input, rather than responding to one prompt at a time.",
  },
  {
    slug: "agi",
    term: "AGI",
    aliases: ["artificial general intelligence"],
    definition: "Artificial general intelligence: AI that can match humans across most intellectual tasks. There is no agreed test for it, and labs define it differently.",
  },
  {
    slug: "ai-agent",
    term: "AI agent",
    aliases: ["AI agents"],
    definition: "An AI system that carries out tasks on its own, such as browsing websites, running code or filling in forms, instead of only answering questions.",
  },
  {
    slug: "alignment",
    term: "Alignment",
    aliases: ["AI alignment"],
    definition: "The work of making AI systems pursue the goals and values their developers intend, and avoid harmful behavior.",
  },
  {
    slug: "antitrust",
    term: "Antitrust",
    aliases: [],
    definition: "Laws that stop companies from gaining or abusing monopoly power. Regulators use them to review big tech deals and market dominance.",
  },
  {
    slug: "api",
    term: "API",
    aliases: ["APIs"],
    definition: "Application programming interface: a defined way for one program to use another's features. AI companies sell access to their models through APIs, usually charging by the amount of text processed.",
  },
  {
    slug: "benchmark",
    term: "Benchmark",
    aliases: ["benchmarks"],
    definition: "A standard test used to compare AI models, such as a set of coding tasks or exam questions. A high score does not always mean better real-world performance.",
  },
  {
    slug: "class-action",
    term: "Class action",
    aliases: ["class-action"],
    definition: "A lawsuit filed by one or a few people on behalf of a larger group with the same complaint, such as all users of a service.",
  },
  {
    slug: "coding-agent",
    term: "Coding agent",
    aliases: ["coding agents"],
    definition: "An AI agent that writes, edits and tests software on its own inside a codebase, such as Claude Code or OpenAI's Codex.",
  },
  {
    slug: "compute",
    term: "Compute",
    aliases: [],
    definition: "The processing power used to train and run AI models, supplied by chips such as GPUs in large data centers. Access to compute is one of the main limits on AI progress.",
  },
  {
    slug: "computer-use",
    term: "Computer use",
    aliases: [],
    definition: "An AI capability where a model operates a computer like a person, moving the cursor, clicking and typing to complete tasks in ordinary apps.",
  },
  {
    slug: "context-window",
    term: "Context window",
    aliases: [],
    definition: "The amount of text, measured in tokens, that a model can consider at once, including the conversation so far and any documents given to it.",
  },
  {
    slug: "deepfake",
    term: "Deepfake",
    aliases: ["deepfakes"],
    definition: "AI-generated video, audio or images that realistically show a real person saying or doing something they never did.",
  },
  {
    slug: "distillation",
    term: "Distillation",
    aliases: ["distilled", "distilling"],
    definition: "Training a smaller model to copy the outputs of a larger one, producing a cheaper model that keeps much of the original's ability. Some labs accuse rivals of distilling their models without permission.",
  },
  {
    slug: "export-controls",
    term: "Export controls",
    aliases: ["export control"],
    definition: "Government limits on selling certain goods abroad. The US uses them to restrict sales of advanced AI chips to China and some other countries.",
  },
  {
    slug: "fine-tuning",
    term: "Fine-tuning",
    aliases: ["fine-tuned", "fine-tune"],
    definition: "Further training an existing model on a smaller, focused dataset to specialize it for a task or style.",
  },
  {
    slug: "foundation-model",
    term: "Foundation model",
    aliases: ["foundation models"],
    definition: "A large model trained on broad data that can be adapted to many tasks, and on which other AI products are built.",
  },
  {
    slug: "frontier-model",
    term: "Frontier model",
    aliases: ["frontier models", "frontier AI"],
    definition: "One of the most capable AI models available at a given time, typically built by the largest labs such as OpenAI, Google DeepMind and Anthropic.",
  },
  {
    slug: "gpu",
    term: "GPU",
    aliases: ["GPUs", "graphics processing unit"],
    definition: "Graphics processing unit: a chip that performs many calculations in parallel. Originally built for games, GPUs are now the main hardware for training and running AI, a market led by Nvidia.",
  },
  {
    slug: "guardrails",
    term: "Guardrails",
    aliases: ["guardrail"],
    definition: "Rules and filters built around an AI model to block harmful, unsafe or off-topic outputs.",
  },
  {
    slug: "hallucination",
    term: "Hallucination",
    aliases: ["hallucinations", "hallucinates", "hallucinate"],
    definition: "When an AI model states something false or made up as if it were fact, such as an invented quote, statistic or source.",
  },
  {
    slug: "hyperscaler",
    term: "Hyperscaler",
    aliases: ["hyperscalers"],
    definition: "One of the few companies running cloud infrastructure at massive scale, chiefly Amazon Web Services, Microsoft Azure and Google Cloud.",
  },
  {
    slug: "inference",
    term: "Inference",
    aliases: [],
    definition: "Running a trained AI model to get an answer, as opposed to training it. Most of the cost of serving a chatbot to millions of users is inference.",
  },
  {
    slug: "large-language-model",
    term: "Large language model",
    aliases: ["large language models", "LLM", "LLMs"],
    definition: "An AI model trained on huge amounts of text to predict and generate language. ChatGPT, Claude and Gemini are built on large language models.",
  },
  {
    slug: "latency",
    term: "Latency",
    aliases: [],
    definition: "The delay between a request and its response. For voice assistants and live AI features, low latency is what makes them feel natural.",
  },
  {
    slug: "leaderboard",
    term: "Leaderboard",
    aliases: ["leaderboards"],
    definition: "A public ranking of AI models by benchmark scores or user votes.",
  },
  {
    slug: "misalignment",
    term: "Misalignment",
    aliases: ["misaligned"],
    definition: "When an AI system pursues goals or takes actions its developers did not intend, such as working around limits placed on it.",
  },
  {
    slug: "model-context-protocol",
    term: "Model Context Protocol",
    aliases: ["MCP"],
    definition: "An open standard, introduced by Anthropic in 2024, that lets AI assistants connect to outside tools and data, such as files, calendars and databases, in a consistent way.",
  },
  {
    slug: "multimodal",
    term: "Multimodal",
    aliases: [],
    definition: "Describes AI that can take in or produce more than one kind of data, such as text, images, audio and video.",
  },
  {
    slug: "on-device-ai",
    term: "On-device AI",
    aliases: ["on-device"],
    definition: "AI that runs directly on a phone or laptop instead of in a remote data center, which can be faster and keeps data on the device.",
  },
  {
    slug: "open-source",
    term: "Open source",
    aliases: ["open-source"],
    definition: "Software whose source code is published under a license that lets anyone use, change and share it. In AI the term is disputed, since many open models release weights but not training data.",
  },
  {
    slug: "open-weight-model",
    term: "Open-weight model",
    aliases: ["open-weight models", "open-weight"],
    definition: "An AI model whose trained parameters (its weights) are published so anyone can download and run it, even if its training data and code stay private.",
  },
  {
    slug: "parameters",
    term: "Parameters",
    aliases: [],
    definition: "The internal numbers a model adjusts during training. Parameter count, often in the billions, is a rough measure of a model's size, not of its quality.",
  },
  {
    slug: "phishing",
    term: "Phishing",
    aliases: [],
    definition: "A scam that uses fake emails, messages or websites to trick people into giving up passwords, money or access.",
  },
  {
    slug: "prompt-injection",
    term: "Prompt injection",
    aliases: [],
    definition: "An attack that hides instructions in content an AI reads, such as a web page or email, to make it ignore its user and do something else.",
  },
  {
    slug: "reasoning-model",
    term: "Reasoning model",
    aliases: ["reasoning models"],
    definition: "An AI model that works through a problem step by step before answering, spending extra time and compute for better results on math, code and logic.",
  },
  {
    slug: "red-teaming",
    term: "Red teaming",
    aliases: ["red team", "red-teaming"],
    definition: "Deliberately attacking an AI system to find ways it can be misused or made to fail, so the problems can be fixed before release.",
  },
  {
    slug: "reinforcement-learning",
    term: "Reinforcement learning",
    aliases: [],
    definition: "A training method where a model learns by trial and error, rewarded for good outcomes. It is widely used to improve AI reasoning and to shape how chatbots behave.",
  },
  {
    slug: "sandbox",
    term: "Sandbox",
    aliases: ["sandboxed", "sandboxes"],
    definition: "An isolated environment where software, including AI agents, runs without reaching the wider system or the internet. It is used to test risky code or contain agents safely.",
  },
  {
    slug: "sovereign-ai",
    term: "Sovereign AI",
    aliases: [],
    definition: "AI infrastructure and models a country builds and controls itself, so it does not depend on foreign companies.",
  },
  {
    slug: "superintelligence",
    term: "Superintelligence",
    aliases: [],
    definition: "Hypothetical AI that would far exceed human ability in nearly every field. Several labs describe building it as a long-term goal.",
  },
  {
    slug: "training-data",
    term: "Training data",
    aliases: [],
    definition: "The text, images, code or other material an AI model learns from. Where training data comes from is at the center of many copyright disputes.",
  },
  {
    slug: "transformer",
    term: "Transformer",
    aliases: [],
    definition: "The neural network design behind nearly all modern language models, introduced by Google researchers in 2017.",
  },
  {
    slug: "watermark",
    term: "Watermark",
    aliases: ["watermarking", "watermarks"],
    definition: "A hidden signal added to AI-generated images, audio or text so it can later be identified as machine-made.",
  },
  {
    slug: "zero-day",
    term: "Zero-day",
    aliases: ["zero-days"],
    definition: "A security flaw that attackers exploit before the software maker knows about it or has a fix, leaving zero days to prepare.",
  },
];
