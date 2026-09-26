"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import type { ApiQuiz } from "../lib/api";
import { trackEvent } from "../lib/analytics";
import { loadProgress, recordResult, shareText, streakOn, type QuizResult } from "../lib/quiz-progress";

const today = () => new Date().toISOString().slice(0, 10);

// One question at a time: pick an answer, see the right one and the story it
// came from, then move on. The result, streak and a spoiler-free share text
// show at the end, and again if the reader comes back the same day.
export function QuizGame({ quiz }: { quiz: ApiQuiz }) {
  const [answers, setAnswers] = useState<number[]>([]);
  const [finished, setFinished] = useState<QuizResult | null>(null);
  const [streak, setStreak] = useState(0);
  const [shared, setShared] = useState("");

  useEffect(() => {
    const progress = loadProgress();
    const played = progress.results[quiz.day];
    if (played && played.number === quiz.number) setFinished(played);
    setStreak(streakOn(progress, today()));
  }, [quiz.day, quiz.number]);

  const index = answers.length;
  const current = quiz.questions[Math.min(index, quiz.questions.length - 1)];
  const [picked, setPicked] = useState<number | null>(null);

  function pick(option: number) {
    if (picked !== null) return;
    setPicked(option);
    trackEvent("quiz_answer", { item_id: String(quiz.number), correct: option === current.answer ? 1 : 0 });
  }

  function next() {
    if (picked === null) return;
    const all = [...answers, picked];
    setAnswers(all);
    setPicked(null);
    if (all.length === quiz.questions.length) {
      const result: QuizResult = {
        day: quiz.day,
        number: quiz.number,
        answers: all,
        correct: all.map((a, i) => a === quiz.questions[i].answer),
      };
      recordResult(result);
      setFinished(result);
      setStreak(streakOn(loadProgress(), today()));
      trackEvent("quiz_complete", { item_id: String(quiz.number), score: result.correct.filter(Boolean).length });
    }
  }

  async function share(result: QuizResult) {
    const text = shareText(result);
    const canShare = typeof navigator.share === "function";
    try {
      if (canShare) await navigator.share({ text });
      else await navigator.clipboard.writeText(text);
      setShared(canShare ? "" : "Copied. Paste it anywhere.");
    } catch {
      setShared(text);
    }
  }

  if (finished) {
    const score = finished.correct.filter(Boolean).length;
    return (
      <div>
        <p className="text-[10px] font-bold uppercase tracking-widest text-accent-purple mb-2">Quiz No. {quiz.number} · done</p>
        <p className="text-6xl font-black tracking-tight">{score}/{finished.correct.length}</p>
        <p className="text-3xl tracking-[0.3em] my-3" aria-label={`${score} of ${finished.correct.length} right`}>
          {finished.correct.map((ok) => (ok ? "🟩" : "🟥")).join("")}
        </p>
        {streak > 0 && <p className="text-text-secondary mb-5">🔥 {streak}-day streak. Come back tomorrow for the next quiz.</p>}
        <button type="button" onClick={() => share(finished)} className="bg-accent-purple hover:brightness-110 text-white text-sm font-bold px-5 py-2.5 rounded-sm">
          Share your score
        </button>
        {shared && <p className="text-text-muted text-sm mt-2 whitespace-pre-line">{shared}</p>}
        <ol className="mt-8 space-y-5">
          {quiz.questions.map((q, i) => (
            <li key={i} className="border-t border-border pt-4">
              <p className="font-bold mb-1">{q.question}</p>
              <p className="text-sm">
                <span className={finished.correct[i] ? "text-accent-green" : "text-red-400"}>{finished.correct[i] ? "Right: " : "Answer: "}</span>
                {q.options[q.answer]}
              </p>
              <Link href={`/article/${q.article.slug}`} className="text-sm text-text-secondary hover:text-accent-purple">
                From: {q.article.title} →
              </Link>
            </li>
          ))}
        </ol>
      </div>
    );
  }

  return (
    <div>
      <p className="text-text-muted text-sm mb-2">
        Question {index + 1} of {quiz.questions.length}
        {streak > 0 && <span> · 🔥 {streak}-day streak</span>}
      </p>
      <h2 className="text-2xl md:text-3xl font-black leading-tight tracking-tight mb-6">{current.question}</h2>
      <div className="grid gap-3">
        {current.options.map((option, i) => {
          const state = picked === null ? "" : i === current.answer ? "right" : i === picked ? "wrong" : "dim";
          return (
            <button
              key={i}
              type="button"
              disabled={picked !== null}
              onClick={() => pick(i)}
              className={`text-left px-4 py-3.5 rounded-sm border font-semibold transition-colors ${
                state === "right"
                  ? "border-accent-green bg-accent-green/15"
                  : state === "wrong"
                    ? "border-red-400 text-red-400"
                    : state === "dim"
                      ? "border-border text-text-muted"
                      : "border-border hover:border-accent-purple"
              }`}
            >
              {option}
            </button>
          );
        })}
      </div>
      {picked !== null && (
        <div className="mt-5 flex flex-wrap items-center justify-between gap-3">
          <p className="text-sm text-text-secondary">
            {picked === current.answer ? "Right. " : "Not quite. "}
            From{" "}
            <Link href={`/article/${current.article.slug}`} className="underline hover:text-accent-purple">
              {current.article.title}
            </Link>
          </p>
          <button type="button" onClick={next} className="bg-white text-black text-sm font-bold px-5 py-2.5 rounded-sm">
            {index + 1 === quiz.questions.length ? "See your score" : "Next question"}
          </button>
        </div>
      )}
    </div>
  );
}
