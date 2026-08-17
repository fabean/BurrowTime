package datetime

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Watson ships a fixed table of full-moon timestamps for 2000–2099. This is
// the same table, compacted as big-endian minute deltas from the first Unix
// timestamp. It is data compatibility, not an astronomical approximation.
const fullMoonBase int64 = 948429720
const fullMoonDeltas = "peKmAqYppmCmnabBprWmgKY8pgal5aXJpaelg6V4pZel4qZFppum06bmptimrKZgpfqlkqVFpSSlM6VvpcumQqbCpzGnZqdApsqmL6WkpT6lAKTspQWlVqXkppynSqerp5enI6aEpeSlW6TypLmkwKURpaemZ6ccp5OnradupuWmNKWEpPuktqS9pQelhqYcpranPKeQp4ynIaZzpb+lO6T3pPSlH6Vqpc+mTabUpzunVqcXpp6mHaWypWulR6VDpWKlpqYDpmSmrabSptKmt6aEpj6l7aWppYeljKWppcul5aYCpjGmcqaupsamrKZupjCmAqXepbiljaVspXGlqKYHpm6mwKbtpvOm1qaXpjSlw6VepSKlGKVBpZKmAqaFpwinYqdrpxamgaXipWOlEKTopO2lI6WXpkGm/6eQp7anaqbUpi2llqUcpMuktKThpVimCabKp2SnrKeVpyumiqXVpTik0aS1pOGlSKXUpm2m+qdop5CnWKbIphWleaUXpPmlD6VKpZ+mDKaLpwKnRKczptemXKXrpZalYqVOpVilhqXRpiqmfaaypsemv6aipmymI6XZpaOlkaWdpbalzKXipgamQaaKpr+myKagpmCmJ6X5pc+lnaVupVqld6XHpjSmm6bjpwOm+6bKpnGl/KWGpTClCqUcpVulwqZDps+nSaeAp1am1KYupZelLKTupN6k/aVUpemmpqdZp7qnoackpn2l2qVPpOmktqTBpRelsaZwpyKnlKeop2Sm2qYspX+k/KS9pMilFqWTpiWmtacxp3yndqcRpmylwqVFpQilCaU1pXql1aZGpr6nHac4pwKmmKYkpcalhaVjpV2lc6WopfemSaaKpq+muqavpo+mV6YOpculoqWbpamlu6XJpd+mD6ZZpqWm0KbHppOmU6YbpeqltaV7pVGlUaWMpfKmZ6bKpwWnFKb3pqymO6W8pUylCqUBpS2lhaX/po6nG6d6p4OnJKaDpdmlU6T/pNqk46UgpZqmSacMp52nwadtps+mJKWKpROkx6S0pOelYqYTptKnZqenp4unH6Z/pc+lOKTXpMCk76VYpeCmb6byp1WneadEpr2mFaWBpSelDaUmpV6lqqYJpnqm5ackpxmmyqZfpfqlr6V/pWqlbaWPpcqmFaZdpo6mqaawpqSmf6ZDpfylwaWlpaOlrKW1pcGl46YkpnqmwqbcpsGmhaZFpgyl0KWQpVWlPaVbpa+mJ6aepvanIacbpuOmfqX5pXilG6T0pQelTaW8pkem3qdep5enZ6bapiiliqUbpOCk06T5pVal8Kayp2anw6elpyGmdKXPpUWk5KS2pMilIaW7pnmnJqePp52nV6bOpiOlfqUCpMek16UnpaKmK6avpyCnZKdfpwGmZ6XJpVOlHKUgpUuliqXZpjqmpab8pxmm76aTpjCl26WjpYGldaWCpaml5qYspmamjaakpqqmnKZypjKl7aW7paalpaWopaulvaXwpkOmn6bdpuSmt6Z0pjKl8qWupWelNqU2pXKl4aZjptanHqcypxOmvqY/pbKlOqT1pOylHaV8pgCmmKcsp5GnlacupoClz6VEpPCkz6TepSGloKZTpxanp6fFp2umyKYapYGlDKTHpLqk8aVtph2m1qdjp5ynfKcRpnalzaU7pOKkzqUBpWil6qZupuWnPadfpy+ms6YYpY6lO6UkpT6lcaWzpgOmZabGpwKm/qbApmSmDqXKpaClhqWApZWlv6X8pjmma6aNpqOmqqaWpmWmIKXfpbalpaWepZuloaXCpgqmbabHpvOm46aopmGmGaXOpYClPqUipUClnKYdpqOnCqc9pzmm+aaGpfWlaaUGpOCk9aVDpbmmTqbsp3KnqadzptymIaV8pQ6k1KTNpPmlW6X5prunb6fHp6SnG6ZrpcWlPqTjpLuk0aUtpcemf6clp4Wnj6dGpsGmH6WApQuk1aTppTmlr6YvpqanCqdJp0Wm8qZlpdOlZqU0pTqlYaWXpdimK6aIptim+qbdppOmPqX2pcOloKWNpYylpKXTpgymQqZtppGmp6atppGmVaYOpdKlrqWdpZOljKWcpdOmMaacpu6nAqbappSmRqX1paOlUqUcpRulXaXTpmOm46c3p1CnLKbLpj+lpaUopOGk26URpXimA6akpz2noqejpzKmfKXDpTek5KTIpNylJqWoplynIKesp8SnZaa/pg+leqUKpMqkw6T9pXqmJ6bXp1unjadqpwGmbaXNpUSk8KTNpQmld6YKpq+nS6ewp6unMqZ1pbilLKTbpMWk4KUupbGmZqclp6unvqdaprOmB6V2pQyk06TPpQ2lh6YuptWnTad5p1Wm8aZnpdGlUKUBpPalKqWIpfemYKa8pwKnJKcGpqemKKWzpW2lXKVvpZKluqXspjCmf6a9ps+msqZ6pj6mC6Xhpbyln6WUpaClw6XypiemXqaTpr6my6aspmemE6XOpZyleqVlpWOliaXhplym2aclpyem6aaNpiilwKVdpQ+k8aUXpYCmFaa1pzGncqdopxemi6XipUqk46TApOClOqW/pmCnB6ePp8Cne6bUpg6lZaT5pMikzqUDpW6mDKbLp3OnwaeSpwWmV6W4pTyk7aTQpO2lS6XdpoSnFKdjp2OnIKarphylkaUqpP2lFKVgpcSmK6aGptOnCacRptima6XzpZalbKVxpYqlqaXMpf+mR6aQpr+mwKaapmamM6YHpd2ltaWYpZGlo6XJpfymNKZxpq2m1qbTpp6mTKX1pbKlg6VipVKlYqWjphamoKcTpz+nHabI"

var moonOnce sync.Once
var moonTimes []int64

func table() []int64 {
	moonOnce.Do(func() {
		raw, err := base64.StdEncoding.DecodeString(fullMoonDeltas)
		if err != nil {
			panic(err)
		}
		moonTimes = make([]int64, 1+len(raw)/2)
		moonTimes[0] = fullMoonBase
		for i := 1; i < len(moonTimes); i++ {
			delta := binary.BigEndian.Uint16(raw[(i-1)*2 : i*2])
			moonTimes[i] = moonTimes[i-1] + int64(delta)*60
		}
	})
	return moonTimes
}

func LastFullMoon(at time.Time) (time.Time, error) {
	timestamps := table()
	unix := at.Unix()
	index := sort.Search(len(timestamps), func(i int) bool { return timestamps[i] > unix })
	if index == 0 || index == len(timestamps) {
		return time.Time{}, fmt.Errorf("watson has only full moon dates from year 2000 to 2099, not %d", at.Year())
	}
	return time.Unix(timestamps[index-1], 0).UTC(), nil
}
