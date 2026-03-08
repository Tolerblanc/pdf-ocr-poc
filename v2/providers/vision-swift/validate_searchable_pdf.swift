import Foundation
import PDFKit

struct OCRDocument: Decodable {
    let pages: [OCRPage]
}

struct OCRPage: Decodable {
    let page: Int
    let text: String
}

struct Thresholds: Encodable {
    let minCoverage: Double
    let minLineMatchRatio: Double

    enum CodingKeys: String, CodingKey {
        case minCoverage = "min_coverage"
        case minLineMatchRatio = "min_line_match_ratio"
    }
}

struct PageValidation: Encodable {
    let page: Int
    let expectedNonBlank: Bool
    let extractedNonBlank: Bool
    let expectedLineCount: Int
    let matchedLineCount: Int
    let lineMatchRatio: Double

    enum CodingKeys: String, CodingKey {
        case page
        case expectedNonBlank = "expected_non_blank"
        case extractedNonBlank = "extracted_non_blank"
        case expectedLineCount = "expected_line_count"
        case matchedLineCount = "matched_line_count"
        case lineMatchRatio = "line_match_ratio"
    }
}

struct ValidationReport: Encodable {
    let ok: Bool
    let searchablePDF: String
    let pagesJSON: String
    let expectedPages: Int
    let extractedPages: Int
    let pageCountMatches: Bool
    let expectedNonBlankPages: Int
    let extractedNonBlankPages: Int
    let coveredNonBlankPages: Int
    let nonBlankCoverage: Double
    let averageLineMatchRatio: Double
    let minLineMatchRatioObserved: Double
    let thresholds: Thresholds
    let failingPages: [PageValidation]
    var failingPageDumpDir: String?
    var failingPageDumps: [PageDump]
    let pages: [PageValidation]

    enum CodingKeys: String, CodingKey {
        case ok
        case searchablePDF = "searchable_pdf"
        case pagesJSON = "pages_json"
        case expectedPages = "expected_pages"
        case extractedPages = "extracted_pages"
        case pageCountMatches = "page_count_matches"
        case expectedNonBlankPages = "expected_non_blank_pages"
        case extractedNonBlankPages = "extracted_non_blank_pages"
        case coveredNonBlankPages = "covered_non_blank_pages"
        case nonBlankCoverage = "non_blank_coverage"
        case averageLineMatchRatio = "average_line_match_ratio"
        case minLineMatchRatioObserved = "min_line_match_ratio_observed"
        case thresholds
        case failingPages = "failing_pages"
        case failingPageDumpDir = "failing_page_dump_dir"
        case failingPageDumps = "failing_page_dumps"
        case pages
    }
}

struct PageDump: Encodable {
    let page: Int
    let expectedPath: String
    let extractedPath: String
    let diffPath: String

    enum CodingKeys: String, CodingKey {
        case page
        case expectedPath = "expected_path"
        case extractedPath = "extracted_path"
        case diffPath = "diff_path"
    }
}

enum ScriptError: Error, CustomStringConvertible {
    case invalidArguments(String)
    case failedToRead(String)
    case failedToDecode(String)
    case failedToOpenPDF(String)

    var description: String {
        switch self {
        case .invalidArguments(let message):
            return message
        case .failedToRead(let path):
            return "failed to read: \(path)"
        case .failedToDecode(let path):
            return "failed to decode JSON: \(path)"
        case .failedToOpenPDF(let path):
            return "failed to open PDF: \(path)"
        }
    }
}

struct Config {
    let searchablePDF: String
    let pagesJSON: String
    let out: String?
    let dumpDir: String?
    let maxDumpPages: Int
    let minCoverage: Double
    let minLineMatchRatio: Double
}

func usage() -> String {
    return "usage: swift validate_searchable_pdf.swift --searchable-pdf <path> --pages-json <path> [--out <path>] [--dump-dir <path>] [--max-dumps <n>] [--min-coverage <0-1>] [--min-line-match <0-1>]"
}

func parseArgs() throws -> Config {
    var searchablePDF = ""
    var pagesJSON = ""
    var out: String?
    var dumpDir: String?
    var maxDumpPages = 5
    var minCoverage = 1.0
    var minLineMatchRatio = 0.85

    var index = 1
    let args = CommandLine.arguments
    while index < args.count {
        let arg = args[index]
        guard index + 1 < args.count else {
            throw ScriptError.invalidArguments(usage())
        }
        let value = args[index + 1]
        switch arg {
        case "--searchable-pdf":
            searchablePDF = value
        case "--pages-json":
            pagesJSON = value
        case "--out":
            out = value
        case "--dump-dir":
            dumpDir = value
        case "--max-dumps":
            guard let parsed = Int(value), parsed >= 0 else {
                throw ScriptError.invalidArguments("--max-dumps must be >= 0")
            }
            maxDumpPages = parsed
        case "--min-coverage":
            guard let parsed = Double(value), parsed >= 0, parsed <= 1 else {
                throw ScriptError.invalidArguments("--min-coverage must be between 0 and 1")
            }
            minCoverage = parsed
        case "--min-line-match":
            guard let parsed = Double(value), parsed >= 0, parsed <= 1 else {
                throw ScriptError.invalidArguments("--min-line-match must be between 0 and 1")
            }
            minLineMatchRatio = parsed
        default:
            throw ScriptError.invalidArguments(usage())
        }
        index += 2
    }

    if searchablePDF.isEmpty || pagesJSON.isEmpty {
        throw ScriptError.invalidArguments(usage())
    }
    if dumpDir == nil {
        let parent = URL(fileURLWithPath: searchablePDF).deletingLastPathComponent().path
        dumpDir = (parent as NSString).appendingPathComponent("searchable_validation_diffs")
    }
    return Config(
        searchablePDF: searchablePDF,
        pagesJSON: pagesJSON,
        out: out,
        dumpDir: dumpDir,
        maxDumpPages: maxDumpPages,
        minCoverage: minCoverage,
        minLineMatchRatio: minLineMatchRatio
    )
}

struct PageComparison {
    let validation: PageValidation
    let expectedText: String
    let extractedText: String
}

struct BuildOutput {
    let report: ValidationReport
    let comparisons: [PageComparison]
}

func writeFile(path: String, content: Data) throws {
    let url = URL(fileURLWithPath: path)
    try FileManager.default.createDirectory(
        at: url.deletingLastPathComponent(),
        withIntermediateDirectories: true
    )
    try content.write(to: url)
}

func normalizeForMatch(_ text: String) -> String {
    let lowered = text.lowercased()
    let collapsed = lowered.replacingOccurrences(of: "\\s+", with: "", options: .regularExpression)
    return collapsed
}

func normalizedLines(_ text: String) -> [String] {
    return text
        .components(separatedBy: .newlines)
        .map(normalizeForMatch)
        .filter { !$0.isEmpty }
}

func normalizedDumpLines(_ text: String) -> [String] {
    var lines = text
        .replacingOccurrences(of: "\r\n", with: "\n")
        .replacingOccurrences(of: "\r", with: "\n")
        .components(separatedBy: "\n")
    while lines.last == "" {
        lines.removeLast()
    }
    return lines
}

enum DiffOp {
    case equal(String)
    case delete(String)
    case insert(String)
}

func lineDiff(expected: [String], extracted: [String]) -> [DiffOp] {
    let m = expected.count
    let n = extracted.count
    var lcs = Array(repeating: Array(repeating: 0, count: n + 1), count: m + 1)

    if m > 0 && n > 0 {
        for i in stride(from: m - 1, through: 0, by: -1) {
            for j in stride(from: n - 1, through: 0, by: -1) {
                if expected[i] == extracted[j] {
                    lcs[i][j] = lcs[i + 1][j + 1] + 1
                } else {
                    lcs[i][j] = max(lcs[i + 1][j], lcs[i][j + 1])
                }
            }
        }
    }

    var ops: [DiffOp] = []
    var i = 0
    var j = 0
    while i < m || j < n {
        if i < m && j < n && expected[i] == extracted[j] {
            ops.append(.equal(expected[i]))
            i += 1
            j += 1
        } else if j < n && (i == m || lcs[i][j + 1] >= lcs[i + 1][j]) {
            ops.append(.insert(extracted[j]))
            j += 1
        } else if i < m {
            ops.append(.delete(expected[i]))
            i += 1
        }
    }
    return ops
}

func renderDiff(page: Int, validation: PageValidation, expectedText: String, extractedText: String) -> String {
    let expectedLines = normalizedDumpLines(expectedText)
    let extractedLines = normalizedDumpLines(extractedText)
    let diff = lineDiff(expected: expectedLines, extracted: extractedLines)

    var output: [String] = []
    output.append("page=\(page)")
    output.append("expected_non_blank=\(validation.expectedNonBlank) extracted_non_blank=\(validation.extractedNonBlank)")
    output.append("expected_line_count=\(validation.expectedLineCount) matched_line_count=\(validation.matchedLineCount) line_match_ratio=\(String(format: "%.4f", validation.lineMatchRatio))")
    output.append("--- expected")
    output.append("+++ extracted")
    for op in diff {
        switch op {
        case .equal(let line):
            output.append(" \(line)")
        case .delete(let line):
            output.append("-\(line)")
        case .insert(let line):
            output.append("+\(line)")
        }
    }
    return output.joined(separator: "\n") + "\n"
}

func validatePage(expected: OCRPage, extractedText: String?) -> PageValidation {
    let extracted = extractedText ?? ""
    let expectedNormalized = normalizeForMatch(expected.text)
    let extractedNormalized = normalizeForMatch(extracted)
    let expectedLines = normalizedLines(expected.text)
    let matchedLines = expectedLines.filter { extractedNormalized.contains($0) }.count
    let lineMatchRatio = expectedLines.isEmpty ? 1.0 : Double(matchedLines) / Double(expectedLines.count)

    return PageValidation(
        page: expected.page,
        expectedNonBlank: !expectedNormalized.isEmpty,
        extractedNonBlank: !extractedNormalized.isEmpty,
        expectedLineCount: expectedLines.count,
        matchedLineCount: matchedLines,
        lineMatchRatio: lineMatchRatio
    )
}

func buildReport(config: Config) throws -> BuildOutput {
    let pagesURL = URL(fileURLWithPath: config.pagesJSON)
    guard let pagesData = try? Data(contentsOf: pagesURL) else {
        throw ScriptError.failedToRead(config.pagesJSON)
    }
    let document: OCRDocument
    do {
        document = try JSONDecoder().decode(OCRDocument.self, from: pagesData)
    } catch {
        throw ScriptError.failedToDecode(config.pagesJSON)
    }

    guard let pdf = PDFDocument(url: URL(fileURLWithPath: config.searchablePDF)) else {
        throw ScriptError.failedToOpenPDF(config.searchablePDF)
    }

    let comparisons = document.pages.map { page -> PageComparison in
        let extractedText = pdf.page(at: page.page - 1)?.string ?? ""
        return PageComparison(
            validation: validatePage(expected: page, extractedText: extractedText),
            expectedText: page.text,
            extractedText: extractedText
        )
    }
    let validations = comparisons.map(\.validation)

    let expectedNonBlankPages = validations.filter { $0.expectedNonBlank }.count
    let extractedNonBlankPages = validations.filter { $0.extractedNonBlank }.count
    let coveredNonBlankPages = validations.filter { $0.expectedNonBlank && $0.extractedNonBlank }.count
    let nonBlankCoverage = expectedNonBlankPages == 0 ? 1.0 : Double(coveredNonBlankPages) / Double(expectedNonBlankPages)

    let nonBlankResults = validations.filter { $0.expectedNonBlank }
    let averageLineMatchRatio: Double
    let minLineMatchRatioObserved: Double
    if nonBlankResults.isEmpty {
        averageLineMatchRatio = 1.0
        minLineMatchRatioObserved = 1.0
    } else {
        averageLineMatchRatio = nonBlankResults.map(\.lineMatchRatio).reduce(0, +) / Double(nonBlankResults.count)
        minLineMatchRatioObserved = nonBlankResults.map(\.lineMatchRatio).min() ?? 1.0
    }

    let failingPages = validations.filter {
        $0.expectedNonBlank && (!$0.extractedNonBlank || $0.lineMatchRatio < config.minLineMatchRatio)
    }
    let pageCountMatches = document.pages.count == pdf.pageCount
    let ok = pageCountMatches && nonBlankCoverage >= config.minCoverage && failingPages.isEmpty

    let report = ValidationReport(
        ok: ok,
        searchablePDF: config.searchablePDF,
        pagesJSON: config.pagesJSON,
        expectedPages: document.pages.count,
        extractedPages: pdf.pageCount,
        pageCountMatches: pageCountMatches,
        expectedNonBlankPages: expectedNonBlankPages,
        extractedNonBlankPages: extractedNonBlankPages,
        coveredNonBlankPages: coveredNonBlankPages,
        nonBlankCoverage: nonBlankCoverage,
        averageLineMatchRatio: averageLineMatchRatio,
        minLineMatchRatioObserved: minLineMatchRatioObserved,
        thresholds: Thresholds(minCoverage: config.minCoverage, minLineMatchRatio: config.minLineMatchRatio),
        failingPages: failingPages,
        failingPageDumpDir: nil,
        failingPageDumps: [],
        pages: validations
    )
    return BuildOutput(report: report, comparisons: comparisons)
}

func writeFailingPageDumps(report: inout ValidationReport, comparisons: [PageComparison], config: Config) throws {
    guard !report.failingPages.isEmpty, config.maxDumpPages > 0, let dumpDir = config.dumpDir else {
        return
    }

    let failingPages = Set(report.failingPages.prefix(config.maxDumpPages).map(\.page))
    guard !failingPages.isEmpty else {
        return
    }

    try FileManager.default.createDirectory(
        at: URL(fileURLWithPath: dumpDir),
        withIntermediateDirectories: true
    )

    var dumps: [PageDump] = []
    for comparison in comparisons where failingPages.contains(comparison.validation.page) {
        let base = String(format: "page-%04d", comparison.validation.page)
        let expectedPath = (dumpDir as NSString).appendingPathComponent("\(base).expected.txt")
        let extractedPath = (dumpDir as NSString).appendingPathComponent("\(base).extracted.txt")
        let diffPath = (dumpDir as NSString).appendingPathComponent("\(base).diff.txt")

        try writeFile(path: expectedPath, content: Data((comparison.expectedText + "\n").utf8))
        try writeFile(path: extractedPath, content: Data((comparison.extractedText + "\n").utf8))
        let diffBody = renderDiff(
            page: comparison.validation.page,
            validation: comparison.validation,
            expectedText: comparison.expectedText,
            extractedText: comparison.extractedText
        )
        try writeFile(path: diffPath, content: Data(diffBody.utf8))

        dumps.append(
            PageDump(
                page: comparison.validation.page,
                expectedPath: expectedPath,
                extractedPath: extractedPath,
                diffPath: diffPath
            )
        )
    }

    report.failingPageDumpDir = dumpDir
    report.failingPageDumps = dumps.sorted { $0.page < $1.page }
}

func writeReport(_ report: ValidationReport, to path: String) throws {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
    var data = try encoder.encode(report)
    data.append(Data("\n".utf8))
    try data.write(to: URL(fileURLWithPath: path))
}

func printSummary(_ report: ValidationReport) {
    print("ok=\(report.ok)")
    print("expected_pages=\(report.expectedPages) extracted_pages=\(report.extractedPages) page_count_matches=\(report.pageCountMatches)")
    print("non_blank_coverage=\(String(format: "%.4f", report.nonBlankCoverage)) covered=\(report.coveredNonBlankPages)/\(report.expectedNonBlankPages)")
    print("average_line_match_ratio=\(String(format: "%.4f", report.averageLineMatchRatio)) min_line_match_ratio=\(String(format: "%.4f", report.minLineMatchRatioObserved))")
    if !report.failingPages.isEmpty {
        let pages = report.failingPages.prefix(10).map { String($0.page) }.joined(separator: ",")
        print("failing_pages=\(pages)")
    }
    if let dumpDir = report.failingPageDumpDir, !report.failingPageDumps.isEmpty {
        print("failing_page_dump_dir=\(dumpDir)")
        print("failing_page_dump_count=\(report.failingPageDumps.count)")
        if let firstDump = report.failingPageDumps.first {
            print("first_diff_path=\(firstDump.diffPath)")
        }
    }
}

do {
    let config = try parseArgs()
    let buildOutput = try buildReport(config: config)
    var report = buildOutput.report
    try writeFailingPageDumps(report: &report, comparisons: buildOutput.comparisons, config: config)
    if let out = config.out {
        try writeReport(report, to: out)
        print("report_path=\(out)")
    }
    printSummary(report)
    if !report.ok {
        exit(1)
    }
} catch {
    fputs("validate_searchable_pdf error: \(error)\n", stderr)
    exit(1)
}
