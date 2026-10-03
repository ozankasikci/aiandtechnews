import Link from "next/link";
import { QuizGame } from "../components/QuizGame";
import { getTodayQuiz } from "../lib/api";

const BASE_URL = "https://www.aiandtech.news";

export const revalidate = 1800;

export const metadata = {
  title: "Daily AI News Quiz",
  description: "Three quick questions on this week's AI and tech news. A new quiz every morning.",
  alternates: { canonical: `${BASE_URL}/quiz` },
};

export default async function QuizPage() {
  const quiz = await getTodayQuiz();
  return (
    <div className="max-w-2xl mx-auto px-4 lg:px-8 py-8">
      <div className="mb-8 pb-6 border-b border-border">
        <h1 className="text-4xl md:text-5xl font-black tracking-tight mb-2">Daily quiz</h1>
        <p className="text-text-secondary text-lg">Three questions on this week&apos;s AI news. A new quiz every morning.</p>
      </div>
      {quiz ? (
        <QuizGame quiz={quiz} />
      ) : (
        <p className="text-text-secondary">
          Today&apos;s quiz isn&apos;t ready yet. Catch up on the <Link href="/" className="text-accent-purple hover:underline">latest stories</Link> in the meantime.
        </p>
      )}
    </div>
  );
}
