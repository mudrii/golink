class Golink < Formula
  desc "LinkedIn CLI for humans and LLM agents"
  homepage "https://github.com/mudrii/golink"
  url "https://github.com/mudrii/golink.git",
      tag: "v26.05.05"
  license "MIT"
  head "https://github.com/mudrii/golink.git", branch: "main"

  depends_on "go" => :build

  def install
    ldflags = %W[
      -s -w
      -X main.version=#{version}
    ]

    system "go", "build", *std_go_args(ldflags: ldflags), "."
  end

  test do
    assert_match "golink #{version}", shell_output("#{bin}/golink version")
    assert_match "LinkedIn CLI for humans and agents", shell_output("#{bin}/golink --help")
  end
end
