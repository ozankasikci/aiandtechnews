interface CategoryTagProps {
  name: string;
  color: string;
}

export function CategoryTag({ name, color }: CategoryTagProps) {
  return (
    <span
      className="inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium"
      style={{
        backgroundColor: `${color}20`,
        color: color,
        border: `1px solid ${color}30`,
      }}
    >
      <span
        className="h-2 w-2 rounded-full"
        style={{ backgroundColor: color }}
      />
      {name}
    </span>
  );
}
