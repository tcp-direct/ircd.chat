package extra

import (
	"bufio"
	"git.tcp.direct/kayos/common/squish"
	"strings"
)

const bandat = "H4sIAAAAAAACA7VWMQ6DMAzc+QIL6gMQUlvhqp9gRWyIlf+vLaVJTHJ2rNBWqhQR++5ytgNVPT36561bl/fivi2onq6fxVxP3br9m+b9qNseNftP3Dju9H7HkRAxFoBhAwZYLPg8Bo/r1sqFjM6YLekTO/zBIS/oBw5xrFKHIEbiUF5fiSOB+HzPfLGKnEh1KL2iNXqyuVDSVAT8cJntAtT4zNmYeUjNOVosAIbtq7n1bAoXS5AyohuLgpTj0Xhl8IAlm4MDvQRn0oRFT9AzeII7OjtIpjIFAmK3uOU+0zmOx9JHkvCeAHMm3KdCG2rTZh7LfGTvSho3scF5MzHAthLv4wbKTFKTAC75vta44/IgRGDIyJqnVa0bD20W9z4eYdNXil0I4EoqIwgpb6ZdHbsv8a3GK4Q1IPWwMpG3l5FfkUMmMsCTZjL+GjITM47gIyRmUJIvCav4PqheojrD3/oKAAA="

func Banner() chan string {
	rd := bufio.NewScanner(strings.NewReader(squish.UnpackStr(bandat)))
	pen := make(chan string)
	go func() {
		for rd.Scan() {
			pen <- rd.Text()
		}
	}()
	return pen
}
