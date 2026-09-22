package artifact

import (
	"bytes"
	"encoding/base64"
	"testing"
)

const go1266V1Archive = "H4sIAAAAAAAE/+yXQWvbMBTH81HCO3utZFtS4uvoobDust5GKc/yU6vVkY2kuoPS7z7kpNB2ECjDpmX+XaQISe8p/7y/lB06ayjEk1+hc6tpYIwxWZZjyxh72zIp8uexxIpxrjhfrdk06bzmPkT0K/bPsVLmLw/3SXiEoG9ph9cD+WA7BxUMHDJAH61BHd+O31nXQAXhzrYtZOBwR1CBp8HSA2Rj57ANZKA7F8nF68beUIhp2S3mQlZlyQtpsBCiLqTaIOelxIZLo2gjyy1tS1OXRufckFK0KTjTTDIjuUY0ZoOQ8tO3dqC/tlaUI26FkmqraylUscmNKJtGFGS02UrecEF5g6apGSlUgtXYcDSsMcpsBEEGrdXkAkEFF+eX6RTYY21bGy0FqH6CJ2wggwdvI8FVBr3vBnLoNEH1CDfkyGPsPFSAN+Tilx36O4p9i5rWGl3nrMZ2jYcveD1weMog3Pd95yM116/1GCMOHK6epvlBeQrdvdc0ze57Ul0oIcb2ZYmkDwde9/k4fS32y6flP6//Z/1Px4qe5hZIGh/zf8be+n/BCrH4/xw8TuQrC58DDIFimDbXVP/v9P+yYIv/z8Fe/9Ped7s+nsTfcYKgSf/j/i/HOQdWjOdCLv4/C/uX+yyhFj4g7f7PzqQ3QKrrd/q/KorF/+fgWf/Tb+dfz77/OJsiZtL/uP8X45wDyf8ZZ8v7fw4uzi/nCLOwsLCw8MH4EwAA//909C7VABoAAA=="

func TestArtifactV1StaticCompatibility(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(go1266V1Archive)
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	const digest = "sha256:7e2aa957679cb657382f54dd53efcf961d15e2dafdb0e7a750bad1af0df7f85e"
	if a.Manifest.ArchiveDigest != digest {
		t.Fatal("fixture digest changed")
	}
	if err := a.VerifyArchiveDigest(raw); err != nil {
		t.Fatal(err)
	}
	ref := LegacyReference{ArchiveDigest: digest, Kind: "skill", Name: "review", ContentDigest: "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"}
	converted, _, err := ConvertV1ToV2(raw, ref, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(converted.Assets["prompt.txt"]) != "review" || string(converted.Licenses["LICENSE"]) != "MIT" {
		t.Fatal("conversion changed protected entries")
	}
}
