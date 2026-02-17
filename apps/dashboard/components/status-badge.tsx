import { cn } from "@/lib/utils";

interface StatusBadgeProps {
  status: "draft" | "published" | "scheduled";
}

const statusConfig = {
  draft: {
    label: "Draft",
    color: "#3b82f6",
  },
  published: {
    label: "Published",
    color: "#22c55e",
  },
  scheduled: {
    label: "Scheduled",
    color: "#f59e0b",
  },
};

export function StatusBadge({ status }: StatusBadgeProps) {
  const config = statusConfig[status];
  return (
    <span
      className="inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium border"
      style={{
        backgroundColor: `${config.color}20`,
        color: config.color,
        borderColor: `${config.color}40`,
      }}
    >
      {config.label}
    </span>
  );
}
