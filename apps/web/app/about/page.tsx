import Image from "next/image";

const TEAM = [
  { name: "Alex Chen", role: "Editor-in-Chief", bio: "Alex leads TechNews's editorial team, covering tech policy and the future of media.", avatar: "https://images.unsplash.com/photo-1472099645785-5658abf4ff4e?w=200&h=200&fit=crop&crop=face" },
  { name: "David Pierce", role: "Editor at Large", bio: "David writes about the gadgets, apps, and platforms that shape how we live and work.", avatar: "https://images.unsplash.com/photo-1500648767791-00dcc994a43e?w=200&h=200&fit=crop&crop=face" },
  { name: "Adi Robertson", role: "Senior Reporter", bio: "Adi covers tech policy, VR, and the intersection of technology and culture.", avatar: "https://images.unsplash.com/photo-1438761681033-6461ffad8d80?w=200&h=200&fit=crop&crop=face" },
  { name: "Emma Roth", role: "News Writer", bio: "Emma covers social media, streaming platforms, and the creator economy.", avatar: "https://images.unsplash.com/photo-1494790108377-be9c29b29330?w=200&h=200&fit=crop&crop=face" },
  { name: "Sean Hollister", role: "Senior Editor", bio: "Sean tests and reviews consumer electronics, from phones to gaming hardware.", avatar: "https://images.unsplash.com/photo-1519345182560-3f2917c472ef?w=200&h=200&fit=crop&crop=face" },
  { name: "Allison Johnson", role: "Reviews Editor", bio: "Allison reviews smartphones, wearables, and other personal technology.", avatar: "https://images.unsplash.com/photo-1580489944761-15a19d654956?w=200&h=200&fit=crop&crop=face" },
];

export default function AboutPage() {
  return (
    <div className="max-w-[800px] mx-auto px-4 lg:px-8 py-12">
      <h1 className="text-4xl md:text-5xl font-black tracking-tight mb-6">About TechNews</h1>

      <div className="border-b border-border pb-8 mb-8">
        <p className="text-text-secondary text-lg leading-relaxed mb-4">
          TechNews covers the intersection of technology, science, art, and culture. Our mission is to explore how technology is shaping the future and changing the way we live, work, and play.
        </p>
        <p className="text-text-secondary text-lg leading-relaxed mb-4">
          TechNews has become one of the most trusted sources for technology news, reviews, and analysis. We believe in rigorous reporting, honest reviews, and putting our audience first.
        </p>
        <p className="text-text-secondary text-lg leading-relaxed">
          We are committed to building an inclusive future for the technology industry and the communities it serves.
        </p>
      </div>

      {/* Team */}
      <section className="mb-12">
        <h2 className="text-xs font-bold uppercase tracking-widest text-text-muted mb-6 pb-2 border-b border-border">Our Team</h2>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          {TEAM.map((member) => (
            <div key={member.name} className="flex gap-4 p-4 bg-bg-card rounded-sm">
              <Image src={member.avatar} alt={member.name} width={64} height={64} className="rounded-full object-cover shrink-0" />
              <div>
                <h3 className="font-bold text-sm">{member.name}</h3>
                <span className="text-accent-purple text-xs font-semibold">{member.role}</span>
                <p className="text-text-secondary text-xs leading-relaxed mt-1">{member.bio}</p>
              </div>
            </div>
          ))}
        </div>
      </section>

      {/* Contact */}
      <section>
        <h2 className="text-xs font-bold uppercase tracking-widest text-text-muted mb-6 pb-2 border-b border-border">Contact Us</h2>
        <div className="bg-bg-card rounded-sm p-6 space-y-3 text-text-secondary text-sm">
          <p><span className="font-semibold text-white">General inquiries:</span> info@technews.dev</p>
          <p><span className="font-semibold text-white">Press:</span> press@technews.dev</p>
          <p><span className="font-semibold text-white">Tips:</span> tips@technews.dev</p>
        </div>
      </section>
    </div>
  );
}
