#!/usr/bin/env bash
# Export Manual/ EPUB + PDF + DOCX via eBookED libraries (sibling clone).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MANUAL="${1:-$ROOT/Manual}"
EBOOKED="${EBOOKED_ROOT:-/Volumes/JohnDovey/Projects/eBookED}"
WORK="${TMPDIR:-/Volumes/JohnDovey/tmp}/walkietalkie-manual-export"
mkdir -p "$WORK"

if [[ ! -f "$EBOOKED/eBookEditor.slnx" ]]; then
  echo "eBookED not found at $EBOOKED (set EBOOKED_ROOT)" >&2
  exit 1
fi
if [[ ! -f "$MANUAL/project.ebookproj.json" ]]; then
  echo "No project.ebookproj.json under $MANUAL" >&2
  exit 1
fi

cat > "$WORK/ExportManual.csproj" <<EOF
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>enable</ImplicitUsings>
    <Nullable>enable</Nullable>
  </PropertyGroup>
  <ItemGroup>
    <ProjectReference Include="$EBOOKED/src/eBookEditor.Core/eBookEditor.Core.csproj" />
    <ProjectReference Include="$EBOOKED/src/eBookEditor.Epub/eBookEditor.Epub.csproj" />
    <ProjectReference Include="$EBOOKED/src/eBookEditor.Pdf/eBookEditor.Pdf.csproj" />
    <ProjectReference Include="$EBOOKED/src/eBookEditor.Html/eBookEditor.Html.csproj" />
    <ProjectReference Include="$EBOOKED/src/eBookEditor.DocxImport/eBookEditor.DocxImport.csproj" />
  </ItemGroup>
</Project>
EOF

cat > "$WORK/Program.cs" <<'EOF'
using eBookEditor.Core.Services;
using eBookEditor.DocxImport.Services;
using eBookEditor.Epub.Services;
using eBookEditor.Html.Services;
using eBookEditor.Pdf.Services;

if (args.Length < 1) {
    Console.Error.WriteLine("usage: ExportManual <manual-project-dir>");
    return 1;
}
var dir = Path.GetFullPath(args[0]);
var project = new ProjectService().LoadProject(dir).Project;
Directory.CreateDirectory(project.OutputDir);
var slug = Slug.Create(project.Metadata.Title, "book");
var epubPath = Path.Combine(project.OutputDir, slug + ".epub");
var pdfPath = Path.Combine(project.OutputDir, slug + ".pdf");
var docxPath = Path.Combine(project.OutputDir, slug + ".docx");

Console.WriteLine($"Exporting from {dir}");
new EpubBuilder().Build(project, epubPath);
Console.WriteLine($"EPUB -> {epubPath}");
var pdf = new PdfBuilder().Build(project, pdfPath);
Console.WriteLine($"PDF  -> {pdfPath} ({pdf.PageCount} pages, {pdf.WordCount} words)");
var html = new HtmlBookAssembler().AssembleWholeBook(project);
var css = new TemplateService().GetTemplateCss(project.Metadata.SelectedTemplate);
new HtmlToDocxConverter().ConvertToFile(html, project.Metadata.Title, docxPath, project.ChaptersDir, css);
Console.WriteLine($"DOCX -> {docxPath}");
return 0;
EOF

# Fix ProjectReference paths for Windows-style escaping in XML — we used absolute paths
# already. Run export.
cd "$WORK"
dotnet run -c Release -- "$MANUAL"
ls -la "$MANUAL/output/"
