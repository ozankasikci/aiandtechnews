export type Article = {
  id: number;
  slug: string;
  tag: string;
  tagColor: string;
  headline: string;
  excerpt: string;
  author: string;
  avatar: string;
  time: string;
  date: string;
  readTime: string;
  image: string;
  imageSource?: string;
  sourceUrl?: string;
  body?: string;
};

export const CATEGORIES: Record<string, { title: string; description: string; color: string }> = {
  tech: { title: "Tech", description: "The latest tech news about hardware, apps, and more", color: "bg-accent-purple" },
  reviews: { title: "Reviews", description: "In-depth reviews of the latest gadgets and software", color: "bg-accent-blue" },
  science: { title: "Science", description: "Discoveries, space exploration, and the natural world", color: "bg-accent-green" },
  entertainment: { title: "Entertainment", description: "Movies, TV, gaming, and streaming culture", color: "bg-accent-magenta" },
  ai: { title: "AI", description: "Artificial intelligence, machine learning, and the future of computing", color: "bg-accent-purple" },
  creators: { title: "Creators", description: "YouTube, TikTok, podcasts, and the creator economy", color: "bg-accent-green" },
};

export const NAV_ITEMS = ["Tech", "Reviews", "Science", "Entertainment", "AI", "Creators"];

export const MOST_POPULAR = [
  "Apple is reportedly building a smart doorbell camera",
  "The best wireless earbuds to buy right now",
  "Microsoft just revealed the next generation of Surface",
  "Tesla's Cybertruck is getting a major software update",
  "Google's new AI can generate music from text",
];

const AUTHORS = [
  { name: "David Pierce", avatar: "https://images.unsplash.com/photo-1472099645785-5658abf4ff4e?w=40&h=40&fit=crop&crop=face" },
  { name: "Adi Robertson", avatar: "https://images.unsplash.com/photo-1438761681033-6461ffad8d80?w=40&h=40&fit=crop&crop=face" },
  { name: "Emma Roth", avatar: "https://images.unsplash.com/photo-1494790108377-be9c29b29330?w=40&h=40&fit=crop&crop=face" },
  { name: "Allison Johnson", avatar: "https://images.unsplash.com/photo-1580489944761-15a19d654956?w=40&h=40&fit=crop&crop=face" },
  { name: "Loren Grush", avatar: "https://images.unsplash.com/photo-1507003211169-0a1dd7228f2d?w=40&h=40&fit=crop&crop=face" },
  { name: "Andrew Webster", avatar: "https://images.unsplash.com/photo-1500648767791-00dcc994a43e?w=40&h=40&fit=crop&crop=face" },
  { name: "Wes Davis", avatar: "https://images.unsplash.com/photo-1506794778202-cad84cf45f1d?w=40&h=40&fit=crop&crop=face" },
  { name: "Sean Hollister", avatar: "https://images.unsplash.com/photo-1519345182560-3f2917c472ef?w=40&h=40&fit=crop&crop=face" },
];

function a(i: number) { const au = AUTHORS[i % AUTHORS.length]; return { author: au.name, avatar: au.avatar }; }

export const ALL_ARTICLES: Article[] = [
  // Tech (8)
  { id: 1, slug: "apple-vision-pro-spatial-computing", tag: "Tech", tagColor: "bg-accent-purple", headline: "Apple Vision Pro is redefining spatial computing — but at what cost?", excerpt: "Apple's $3,499 headset is the most ambitious product the company has made in years. We spent a week with it.", ...a(0), time: "2 hours ago", date: "Feb 15, 2026", readTime: "8 min read", image: "https://images.unsplash.com/photo-1622979135225-d2ba269cf1ac?w=800&h=500&fit=crop" },
  { id: 2, slug: "usb-c-everywhere-finally", tag: "Tech", tagColor: "bg-accent-purple", headline: "USB-C is finally everywhere, and it only took a decade", excerpt: "The EU mandate has finally pushed every major manufacturer to adopt the universal standard.", ...a(6), time: "3 hours ago", date: "Feb 15, 2026", readTime: "5 min read", image: "https://images.unsplash.com/photo-1625842268584-8f3296236761?w=800&h=500&fit=crop" },
  { id: 3, slug: "windows-12-first-look", tag: "Tech", tagColor: "bg-accent-purple", headline: "Windows 12 first look: Microsoft's AI-powered operating system", excerpt: "Copilot is baked into everything, and the new Start menu is actually good this time.", ...a(7), time: "4 hours ago", date: "Feb 14, 2026", readTime: "6 min read", image: "https://images.unsplash.com/photo-1624571409412-1f253e1ecc89?w=800&h=500&fit=crop" },
  { id: 4, slug: "nothing-phone-3-design", tag: "Tech", tagColor: "bg-accent-purple", headline: "Nothing Phone 3 has the wildest design we've ever seen", excerpt: "Carl Pei's latest creation takes transparent design to a whole new level with an e-ink back panel.", ...a(3), time: "5 hours ago", date: "Feb 14, 2026", readTime: "4 min read", image: "https://images.unsplash.com/photo-1511707171634-5f897ff02aa9?w=800&h=500&fit=crop" },
  { id: 5, slug: "starlink-direct-to-cell", tag: "Tech", tagColor: "bg-accent-purple", headline: "Starlink's direct-to-cell service is now live in 15 countries", excerpt: "SpaceX's satellite internet can now connect directly to unmodified smartphones anywhere on Earth.", ...a(0), time: "6 hours ago", date: "Feb 13, 2026", readTime: "5 min read", image: "https://images.unsplash.com/photo-1516849841032-87cbac4d88f7?w=800&h=500&fit=crop" },
  { id: 6, slug: "meta-ray-ban-ai-update", tag: "Tech", tagColor: "bg-accent-purple", headline: "Meta's Ray-Ban smart glasses just got a huge AI upgrade", excerpt: "Real-time translation, object recognition, and a new multimodal AI assistant are coming to the glasses.", ...a(1), time: "7 hours ago", date: "Feb 13, 2026", readTime: "6 min read", image: "https://images.unsplash.com/photo-1572635196237-14b3f281503f?w=800&h=500&fit=crop" },
  { id: 7, slug: "raspberry-pi-6-announced", tag: "Tech", tagColor: "bg-accent-purple", headline: "Raspberry Pi 6 packs desktop-class performance for $60", excerpt: "The new Pi features an octa-core ARM chip and can actually handle 4K video editing.", ...a(6), time: "8 hours ago", date: "Feb 12, 2026", readTime: "4 min read", image: "https://images.unsplash.com/photo-1518770660439-4636190af475?w=800&h=500&fit=crop" },
  { id: 8, slug: "bluetooth-6-standard", tag: "Tech", tagColor: "bg-accent-purple", headline: "Bluetooth 6.0 promises lossless audio and centimeter-level tracking", excerpt: "The next generation of Bluetooth will finally deliver on promises the standard has been making for years.", ...a(7), time: "9 hours ago", date: "Feb 12, 2026", readTime: "5 min read", image: "https://images.unsplash.com/photo-1606220945770-b5b6c2c55bf1?w=800&h=500&fit=crop" },

  // Reviews (8)
  { id: 10, slug: "samsung-galaxy-s25-ultra-review", tag: "Reviews", tagColor: "bg-accent-blue", headline: "Samsung Galaxy S25 Ultra review: the AI phone", excerpt: "Samsung's latest flagship is packed with AI features that actually make it worth upgrading.", ...a(3), time: "3 hours ago", date: "Feb 15, 2026", readTime: "12 min read", image: "https://images.unsplash.com/photo-1610945265064-0e34e5519bbf?w=800&h=500&fit=crop" },
  { id: 11, slug: "macbook-air-m4-review", tag: "Reviews", tagColor: "bg-accent-blue", headline: "MacBook Air M4 review: Apple's best laptop gets even better", excerpt: "Faster, cooler, and now with a better webcam. The MacBook Air M4 is the laptop to beat.", ...a(0), time: "5 hours ago", date: "Feb 14, 2026", readTime: "10 min read", image: "https://images.unsplash.com/photo-1517336714731-489689fd1ca8?w=800&h=500&fit=crop" },
  { id: 12, slug: "sony-wh-1000xm6-review", tag: "Reviews", tagColor: "bg-accent-blue", headline: "Sony WH-1000XM6 review: still the noise-canceling king", excerpt: "Sony's flagship headphones get better battery life and improved ANC with a lighter design.", ...a(7), time: "6 hours ago", date: "Feb 14, 2026", readTime: "8 min read", image: "https://images.unsplash.com/photo-1505740420928-5e560c06d30e?w=800&h=500&fit=crop" },
  { id: 13, slug: "ipad-pro-m5-review", tag: "Reviews", tagColor: "bg-accent-blue", headline: "iPad Pro M5 review: the tablet that wants to be your only computer", excerpt: "With macOS app support finally here, the iPad Pro makes a real case for replacing your laptop.", ...a(3), time: "8 hours ago", date: "Feb 13, 2026", readTime: "11 min read", image: "https://images.unsplash.com/photo-1544244015-0df4b3ffc6b0?w=800&h=500&fit=crop" },
  { id: 14, slug: "pixel-watch-4-review", tag: "Reviews", tagColor: "bg-accent-blue", headline: "Pixel Watch 4 review: Google's best wearable yet", excerpt: "Better battery, brighter display, and Gemini on your wrist make this the Wear OS watch to get.", ...a(1), time: "10 hours ago", date: "Feb 13, 2026", readTime: "7 min read", image: "https://images.unsplash.com/photo-1523275335684-37898b6baf30?w=800&h=500&fit=crop" },
  { id: 15, slug: "steam-deck-2-review", tag: "Reviews", tagColor: "bg-accent-blue", headline: "Steam Deck 2 review: Valve's handheld grows up", excerpt: "An OLED display, faster chip, and better ergonomics make this the definitive portable PC.", ...a(5), time: "12 hours ago", date: "Feb 12, 2026", readTime: "9 min read", image: "https://images.unsplash.com/photo-1612287230202-1ff1d85d1bdf?w=800&h=500&fit=crop" },
  { id: 16, slug: "lg-c4-oled-review", tag: "Reviews", tagColor: "bg-accent-blue", headline: "LG C4 OLED review: the best TV for most people", excerpt: "Brighter, more colorful, and still the best value in OLED TVs for gaming and movies alike.", ...a(6), time: "14 hours ago", date: "Feb 12, 2026", readTime: "8 min read", image: "https://images.unsplash.com/photo-1593359677879-a4bb92f829d1?w=800&h=500&fit=crop" },
  { id: 17, slug: "dyson-ontrac-review", tag: "Reviews", tagColor: "bg-accent-blue", headline: "Dyson OnTrac review: $500 headphones that sound incredible", excerpt: "Dyson's first headphones are over-engineered, overpriced, and surprisingly great.", ...a(7), time: "16 hours ago", date: "Feb 11, 2026", readTime: "7 min read", image: "https://images.unsplash.com/photo-1583394838336-acd977736f90?w=800&h=500&fit=crop" },

  // Science (8)
  { id: 20, slug: "nasa-mars-sample-return-redesign", tag: "Science", tagColor: "bg-accent-green", headline: "NASA's next Mars mission just got a major redesign", excerpt: "The Mars Sample Return mission is getting a complete overhaul after budget concerns.", ...a(4), time: "2 hours ago", date: "Feb 15, 2026", readTime: "6 min read", image: "https://images.unsplash.com/photo-1614728263952-84ea256f9679?w=800&h=500&fit=crop" },
  { id: 21, slug: "james-webb-exoplanet-atmosphere", tag: "Science", tagColor: "bg-accent-green", headline: "James Webb telescope finds water vapor in a rocky exoplanet's atmosphere", excerpt: "The discovery marks the first time water has been detected on a planet this small outside our solar system.", ...a(4), time: "4 hours ago", date: "Feb 15, 2026", readTime: "7 min read", image: "https://images.unsplash.com/photo-1462331940025-496dfbfc7564?w=800&h=500&fit=crop" },
  { id: 22, slug: "crispr-gene-therapy-breakthrough", tag: "Science", tagColor: "bg-accent-green", headline: "New CRISPR therapy cures sickle cell disease in landmark trial", excerpt: "The one-time treatment has shown complete remission in 95% of patients after two years.", ...a(1), time: "5 hours ago", date: "Feb 14, 2026", readTime: "8 min read", image: "https://images.unsplash.com/photo-1532187863486-abf9dbad1b69?w=800&h=500&fit=crop" },
  { id: 23, slug: "deep-ocean-discovery-species", tag: "Science", tagColor: "bg-accent-green", headline: "Scientists discover 50 new species in the deepest ocean trench", excerpt: "A robotic expedition to the Mariana Trench reveals creatures never before seen by humans.", ...a(4), time: "7 hours ago", date: "Feb 14, 2026", readTime: "5 min read", image: "https://images.unsplash.com/photo-1518399681705-1c1a55e5e883?w=800&h=500&fit=crop" },
  { id: 24, slug: "fusion-energy-record-broken", tag: "Science", tagColor: "bg-accent-green", headline: "Fusion energy record shattered — net positive energy sustained for 10 minutes", excerpt: "A European research facility has achieved a milestone that brings commercial fusion closer to reality.", ...a(0), time: "9 hours ago", date: "Feb 13, 2026", readTime: "6 min read", image: "https://images.unsplash.com/photo-1635070041078-e363dbe005cb?w=800&h=500&fit=crop" },
  { id: 25, slug: "asteroid-mining-first-mission", tag: "Science", tagColor: "bg-accent-green", headline: "The first commercial asteroid mining mission launches next month", excerpt: "AstroForge is sending a spacecraft to extract platinum-group metals from a near-Earth asteroid.", ...a(6), time: "11 hours ago", date: "Feb 13, 2026", readTime: "7 min read", image: "https://images.unsplash.com/photo-1446776811953-b23d57bd21aa?w=800&h=500&fit=crop" },
  { id: 26, slug: "brain-computer-interface-paralysis", tag: "Science", tagColor: "bg-accent-green", headline: "Brain-computer interface lets paralyzed patient control robotic arm with thoughts", excerpt: "The wireless implant translates neural signals into precise robotic movements in real time.", ...a(1), time: "13 hours ago", date: "Feb 12, 2026", readTime: "6 min read", image: "https://images.unsplash.com/photo-1559757175-5700dde675bc?w=800&h=500&fit=crop" },
  { id: 27, slug: "climate-tipping-point-study", tag: "Science", tagColor: "bg-accent-green", headline: "New study warns we're closer to climate tipping points than previously thought", excerpt: "Researchers say the Amazon rainforest and Arctic ice sheets could reach irreversible thresholds by 2030.", ...a(4), time: "15 hours ago", date: "Feb 12, 2026", readTime: "9 min read", image: "https://images.unsplash.com/photo-1569163139394-de4e4f43e4e3?w=800&h=500&fit=crop" },

  // Entertainment (8)
  { id: 30, slug: "best-streaming-this-weekend", tag: "Entertainment", tagColor: "bg-accent-magenta", headline: "The best movies and TV shows streaming this weekend", excerpt: "From a new sci-fi thriller on Netflix to a critically acclaimed drama on Apple TV Plus.", ...a(5), time: "2 hours ago", date: "Feb 15, 2026", readTime: "5 min read", image: "https://images.unsplash.com/photo-1536440136628-849c177e76a1?w=800&h=500&fit=crop" },
  { id: 31, slug: "gta-6-preview-hands-on", tag: "Entertainment", tagColor: "bg-accent-magenta", headline: "We played GTA 6 for three hours — here's what we think", excerpt: "Rockstar's long-awaited sequel lives up to the hype with a stunning open world and sharp writing.", ...a(5), time: "4 hours ago", date: "Feb 15, 2026", readTime: "10 min read", image: "https://images.unsplash.com/photo-1493711662062-fa541adb3fc8?w=800&h=500&fit=crop" },
  { id: 32, slug: "nintendo-switch-2-games-lineup", tag: "Entertainment", tagColor: "bg-accent-magenta", headline: "Nintendo Switch 2's launch lineup has 20 confirmed games", excerpt: "Mario, Zelda, and a surprise new IP will be available when the console launches this spring.", ...a(5), time: "6 hours ago", date: "Feb 14, 2026", readTime: "6 min read", image: "https://images.unsplash.com/photo-1578303512597-81e6cc155b3e?w=800&h=500&fit=crop" },
  { id: 33, slug: "spotify-ai-dj-podcast", tag: "Entertainment", tagColor: "bg-accent-magenta", headline: "Spotify's AI DJ can now host your podcast", excerpt: "The feature generates a personalized audio experience that blends music and talk content.", ...a(2), time: "8 hours ago", date: "Feb 14, 2026", readTime: "4 min read", image: "https://images.unsplash.com/photo-1614680376593-902f74cf0d41?w=800&h=500&fit=crop" },
  { id: 34, slug: "marvel-phase-7-slate", tag: "Entertainment", tagColor: "bg-accent-magenta", headline: "Marvel just revealed its entire Phase 7 slate", excerpt: "The MCU is getting a soft reboot with a focus on new characters and smaller, more personal stories.", ...a(5), time: "10 hours ago", date: "Feb 13, 2026", readTime: "7 min read", image: "https://images.unsplash.com/photo-1635805737707-575885ab0820?w=800&h=500&fit=crop" },
  { id: 35, slug: "apple-tv-plus-sports-deal", tag: "Entertainment", tagColor: "bg-accent-magenta", headline: "Apple TV Plus just landed a massive Premier League deal", excerpt: "Apple is paying $2 billion per season for exclusive streaming rights starting in 2027.", ...a(1), time: "12 hours ago", date: "Feb 13, 2026", readTime: "5 min read", image: "https://images.unsplash.com/photo-1522778119026-d647f0596c20?w=800&h=500&fit=crop" },
  { id: 36, slug: "ai-generated-movie-controversy", tag: "Entertainment", tagColor: "bg-accent-magenta", headline: "The first fully AI-generated movie hits theaters — and critics hate it", excerpt: "The film cost $2 million to make but has drawn intense backlash from Hollywood guilds.", ...a(5), time: "14 hours ago", date: "Feb 12, 2026", readTime: "8 min read", image: "https://images.unsplash.com/photo-1440404653325-ab127d49abc1?w=800&h=500&fit=crop" },
  { id: 37, slug: "playstation-vr3-announcement", tag: "Entertainment", tagColor: "bg-accent-magenta", headline: "Sony announces PlayStation VR3 with eye-tracking and haptic gloves", excerpt: "The next-gen VR headset promises a leap in immersion with new controllers and mixed reality.", ...a(7), time: "16 hours ago", date: "Feb 12, 2026", readTime: "6 min read", image: "https://images.unsplash.com/photo-1622979135225-d2ba269cf1ac?w=800&h=500&fit=crop" },

  // AI (8)
  { id: 40, slug: "google-gemini-ultra-2", tag: "AI", tagColor: "bg-accent-purple", headline: "Google's Gemini Ultra 2 can reason through complex math like a PhD student", excerpt: "The new model scores higher than most humans on graduate-level mathematics and science tests.", ...a(0), time: "2 hours ago", date: "Feb 15, 2026", readTime: "7 min read", image: "https://images.unsplash.com/photo-1677442136019-21780ecad995?w=800&h=500&fit=crop" },
  { id: 41, slug: "openai-agents-platform", tag: "AI", tagColor: "bg-accent-purple", headline: "OpenAI launches an agent platform that can browse the web for you", excerpt: "The new Operator feature can book flights, fill out forms, and make purchases autonomously.", ...a(0), time: "4 hours ago", date: "Feb 15, 2026", readTime: "6 min read", image: "https://images.unsplash.com/photo-1620712943543-bcc4688e7485?w=800&h=500&fit=crop" },
  { id: 42, slug: "anthropic-claude-4-release", tag: "AI", tagColor: "bg-accent-purple", headline: "Anthropic's Claude 4 sets new benchmarks in coding and reasoning", excerpt: "The latest model from Anthropic can write production-ready code and debug complex systems.", ...a(6), time: "6 hours ago", date: "Feb 14, 2026", readTime: "8 min read", image: "https://images.unsplash.com/photo-1555255707-c07966088b7b?w=800&h=500&fit=crop" },
  { id: 43, slug: "ai-regulation-eu-act", tag: "AI", tagColor: "bg-accent-purple", headline: "The EU AI Act is now in full effect — here's what changes", excerpt: "Companies face hefty fines for non-compliance as Europe's landmark AI regulation comes online.", ...a(1), time: "8 hours ago", date: "Feb 14, 2026", readTime: "6 min read", image: "https://images.unsplash.com/photo-1529070538774-1f9e5e8e2df4?w=800&h=500&fit=crop" },
  { id: 44, slug: "midjourney-video-generator", tag: "AI", tagColor: "bg-accent-purple", headline: "Midjourney can now generate cinematic video clips", excerpt: "The AI art company expands into video with stunning 30-second clips from text prompts.", ...a(2), time: "10 hours ago", date: "Feb 13, 2026", readTime: "5 min read", image: "https://images.unsplash.com/photo-1633356122544-f134324a6cee?w=800&h=500&fit=crop" },
  { id: 45, slug: "ai-music-copyright-ruling", tag: "AI", tagColor: "bg-accent-purple", headline: "Court rules AI-generated music can't be copyrighted", excerpt: "A federal judge says works created entirely by artificial intelligence lack human authorship.", ...a(1), time: "12 hours ago", date: "Feb 13, 2026", readTime: "5 min read", image: "https://images.unsplash.com/photo-1511379938547-c1f69419868d?w=800&h=500&fit=crop" },
  { id: 46, slug: "nvidia-ai-chip-next-gen", tag: "AI", tagColor: "bg-accent-purple", headline: "Nvidia's next-gen AI chip is 3x faster and uses half the power", excerpt: "The Blackwell Ultra GPU is designed for the next wave of AI training and inference workloads.", ...a(7), time: "14 hours ago", date: "Feb 12, 2026", readTime: "6 min read", image: "https://images.unsplash.com/photo-1591238372338-23a40a3d5e1b?w=800&h=500&fit=crop" },
  { id: 47, slug: "ai-doctor-diagnosis-study", tag: "AI", tagColor: "bg-accent-purple", headline: "AI outperforms doctors in diagnosing rare diseases, study finds", excerpt: "A new study shows AI systems can identify rare conditions faster and more accurately than specialists.", ...a(4), time: "16 hours ago", date: "Feb 12, 2026", readTime: "7 min read", image: "https://images.unsplash.com/photo-1576091160399-112ba8d25d1d?w=800&h=500&fit=crop" },

  // Creators (8)
  { id: 50, slug: "youtube-tiktok-feed-shorts", tag: "Creators", tagColor: "bg-accent-green", headline: "YouTube is testing a TikTok-like feed for Shorts", excerpt: "The new feed makes Shorts feel more like an endless scroll of short-form video content.", ...a(2), time: "2 hours ago", date: "Feb 15, 2026", readTime: "4 min read", image: "https://images.unsplash.com/photo-1611162616475-46b635cb6868?w=800&h=500&fit=crop" },
  { id: 51, slug: "tiktok-creator-fund-2", tag: "Creators", tagColor: "bg-accent-green", headline: "TikTok launches Creator Fund 2.0 with much better payouts", excerpt: "Creators can now earn up to 10x more per view under the revamped monetization program.", ...a(2), time: "4 hours ago", date: "Feb 15, 2026", readTime: "5 min read", image: "https://images.unsplash.com/photo-1611605698335-8b1569810432?w=800&h=500&fit=crop" },
  { id: 52, slug: "instagram-subscription-features", tag: "Creators", tagColor: "bg-accent-green", headline: "Instagram adds Patreon-like subscription tiers for creators", excerpt: "Creators can now offer exclusive content at multiple price points directly through the app.", ...a(2), time: "6 hours ago", date: "Feb 14, 2026", readTime: "4 min read", image: "https://images.unsplash.com/photo-1611262588024-d12430b98920?w=800&h=500&fit=crop" },
  { id: 53, slug: "mrbeast-biggest-video-ever", tag: "Creators", tagColor: "bg-accent-green", headline: "MrBeast's latest video cost $10 million and broke every YouTube record", excerpt: "The 45-minute production featured 1,000 contestants competing for a private island.", ...a(5), time: "8 hours ago", date: "Feb 14, 2026", readTime: "6 min read", image: "https://images.unsplash.com/photo-1492619375914-88005aa9e8fb?w=800&h=500&fit=crop" },
  { id: 54, slug: "podcast-industry-growth-2026", tag: "Creators", tagColor: "bg-accent-green", headline: "Podcasting hits $4 billion in revenue as video podcasts surge", excerpt: "The industry is growing faster than ever, driven by YouTube and Spotify's push into video.", ...a(0), time: "10 hours ago", date: "Feb 13, 2026", readTime: "5 min read", image: "https://images.unsplash.com/photo-1478737270239-2f02b77fc618?w=800&h=500&fit=crop" },
  { id: 55, slug: "twitch-new-monetization", tag: "Creators", tagColor: "bg-accent-green", headline: "Twitch overhauls monetization with new ad revenue split", excerpt: "Streamers will now keep 70% of ad revenue in a move to stem the exodus to YouTube.", ...a(7), time: "12 hours ago", date: "Feb 13, 2026", readTime: "4 min read", image: "https://images.unsplash.com/photo-1542751371-adc38448a05e?w=800&h=500&fit=crop" },
  { id: 56, slug: "substack-video-expansion", tag: "Creators", tagColor: "bg-accent-green", headline: "Substack is going all-in on video to compete with YouTube", excerpt: "The newsletter platform is adding full video hosting, analytics, and monetization tools.", ...a(0), time: "14 hours ago", date: "Feb 12, 2026", readTime: "5 min read", image: "https://images.unsplash.com/photo-1504711434969-e33886168d5c?w=800&h=500&fit=crop" },
  { id: 57, slug: "creator-burnout-study", tag: "Creators", tagColor: "bg-accent-green", headline: "90% of full-time creators report burnout, new study reveals", excerpt: "The pressure to constantly produce content is taking a serious toll on mental health.", ...a(1), time: "16 hours ago", date: "Feb 12, 2026", readTime: "7 min read", image: "https://images.unsplash.com/photo-1499750310107-5fef28a66643?w=800&h=500&fit=crop" },

];

export function getArticlesByCategory(category: string): Article[] {
  return ALL_ARTICLES.filter((a) => a.tag.toLowerCase() === category.toLowerCase());
}

export function getArticleBySlug(slug: string): Article | undefined {
  return ALL_ARTICLES.find((a) => a.slug === slug);
}

export const SAMPLE_ARTICLE_BODY = `
<p>The technology industry is at a crossroads. After years of rapid innovation and growth, companies are now grappling with new challenges that will define the next decade of computing.</p>

<p>From the rise of artificial intelligence to the push for more sustainable hardware, the changes happening right now will affect every aspect of how we interact with technology. And the pace of change isn't slowing down — if anything, it's accelerating.</p>

<h2>A new era of computing</h2>

<p>The shift toward AI-first computing is perhaps the most significant change we've seen since the smartphone revolution. Every major tech company is now reorganizing around AI, from how they build products to how they think about user experience.</p>

<blockquote>"We're seeing the beginning of a fundamental transformation in how humans interact with computers. The interface is becoming invisible." — Industry analyst</blockquote>

<p>This transformation isn't just about chatbots and image generators. It's about rethinking every layer of the computing stack, from chips designed specifically for AI workloads to operating systems that anticipate what you need before you ask.</p>

<h2>What this means for consumers</h2>

<p>For everyday users, the most visible change will be in how devices understand and respond to natural language. Instead of navigating menus and settings, you'll simply tell your device what you want to do.</p>

<p>But there are concerns. Privacy advocates worry about the amount of data these AI systems need to function effectively. And there are legitimate questions about reliability — what happens when the AI gets it wrong?</p>

<p>These are questions the industry will need to answer as it pushes forward into this new era. The companies that get it right will define the next generation of technology. The ones that don't will be left behind.</p>

<h2>Looking ahead</h2>

<p>The next 12 months will be critical. We'll see the first wave of truly AI-native devices, new regulations coming into effect, and a continued push toward more open and interoperable standards.</p>

<p>One thing is clear: the technology landscape of 2027 will look very different from what we see today. And the changes are already underway.</p>
`;
