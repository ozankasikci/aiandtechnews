import Link from "next/link";
import { getTodayQuiz, type ListCache } from "../lib/api";

// A sidebar invitation to today's quiz; nothing when no quiz is published.
export async function QuizCard({ cache = "list" }: { cache?: ListCache } = {}) {
  const quiz = await getTodayQuiz(cache);
  if (!quiz) return null;
  return (
    <div className="mt-8 bg-bg-card border border-border rounded-sm p-5">
      <p className="text-[10px] font-bold uppercase tracking-widest text-accent-purple mb-1">Daily quiz · No. {quiz.number}</p>
      <p className="text-lg font-black leading-tight mb-1">{quiz.questions.length} questions on this week&apos;s AI news</p>
      <p className="text-text-secondary text-sm mb-4">Takes a minute. Keep your streak going.</p>
      <Link href="/quiz" className="inline-block bg-accent-purple hover:brightness-110 transition-all text-white text-sm font-bold px-5 py-2.5 rounded-sm">
        Play today&apos;s quiz
      </Link>
    </div>
  );
}
