export function TableEmptyRow({
  colSpan,
  title,
  description,
}: {
  colSpan: number;
  title: string;
  description?: string;
}) {
  return (
    <tr>
      <td colSpan={colSpan} className="px-4 py-8 text-center">
        <div className="space-y-1">
          <p className="font-medium">{title}</p>
          {description ? <p className="text-sm text-muted-foreground">{description}</p> : null}
        </div>
      </td>
    </tr>
  );
}
