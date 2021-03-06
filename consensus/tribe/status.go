package tribe

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/MeshBoxFoundation/meshbox/common"
	"github.com/MeshBoxFoundation/meshbox/core/types"
	"github.com/MeshBoxFoundation/meshbox/crypto"
	"github.com/MeshBoxFoundation/meshbox/log"
	"github.com/MeshBoxFoundation/meshbox/params"
)

func NewTribeStatus() *TribeStatus {
	ts := &TribeStatus{
		Signers:     make([]*Signer, 0),
		SignerLevel: LevelNone,
	}
	return ts
}

func (self *TribeStatus) SetTribe(tribe *Tribe) {
	self.tribe = tribe
}

func (self *TribeStatus) getNodekey() *ecdsa.PrivateKey {
	if self.nodeKey == nil {
		panic(errors.New("GetNodekey but nodekey not ready"))
	}
	return self.nodeKey
}

func (self *TribeStatus) SetNodeKey(nodeKey *ecdsa.PrivateKey) {
	self.nodeKey = nodeKey
}

func (self *TribeStatus) GetNodeKey() *ecdsa.PrivateKey {
	return self.nodeKey
}

func (self *TribeStatus) GetMinerAddress() common.Address {
	if self.nodeKey == nil {
		panic(errors.New("GetMinerAddress but nodekey not ready"))
	}
	pub := self.nodeKey.PublicKey
	add := crypto.PubkeyToAddress(pub)
	return add
}
func (self *TribeStatus) IsLeader(addr common.Address) bool {
	for _, a := range self.Leaders {
		if a == addr {
			return true
		}
	}
	return false
}
func (self *TribeStatus) GetMinerAddressByChan(rtn chan common.Address) {
	go func() {
		for {
			if self.nodeKey != nil && self.tribe.isInit {
				break
			}
			<-time.After(time.Second)
		}
		pub := self.nodeKey.PublicKey
		rtn <- crypto.PubkeyToAddress(pub)
	}()
}

//func (self *TribeStatus) GetSignersFromChiefByHash(hash common.Hash, number *big.Int) ([]*Signer, error) {
//	sc, ok := self.signersCache.Get(hash)
//	if ok {
//		return sc.([]*Signer), nil
//	}
//	rtn := params.SendToMsgBoxWithHash("GetStatus", hash, number)
//	r := <-rtn
//	if !r.Success {
//		return nil, r.Entity.(error)
//	}
//	cs := r.Entity.(params.ChiefStatus)
//	signers := cs.SignerList
//	scores := cs.ScoreList
//	sl := make([]*Signer, 0, len(signers))
//	for i, signer := range signers {
//		score := scores[i]
//		sl = append(sl, &Signer{signer, score.Int64()})
//	}
//	self.signersCache.Add(hash, sl)
//	return sl, nil
//}

// ???????????????prepare??????????????????????????????????????????????????????????????????????????????????????????????????????????????????????????????????????????????????????????????????????????????????????????
func (self *TribeStatus) LoadStatusFromChief(hash common.Hash, number *big.Int) error {
	//log.Info(fmt.Sprintf("LoadSignersFromChief hash=%s,number=%s", hash.String(), number))
	cs, err := params.TribeGetStatus(number, hash)
	if err != nil {
		log.Warn("TribeGetStatusError", "err", err, "num", number, "hash", hash.Hex())
		return err
	}
	signers := cs.SignerList
	scores := cs.ScoreList
	sl := make([]*Signer, 0, len(signers))
	for i, signer := range signers {
		score := scores[i]
		sl = append(sl, &Signer{signer, score.Int64()})
	}
	self.LeaderLimit = cs.LeaderLimit
	self.Leaders = cs.LeaderList
	if len(self.Leaders) == 0 && params.IsSIP100Block(number) {
		panic(fmt.Sprintf("LoadSignersFromChief err ,hash=%s,number=%s,cs=%#v", hash.String(), number, cs))
	}
	self.Number = cs.Number.Int64()
	self.blackList = cs.BlackList
	self.loadSigners(sl)
	self.Epoch, self.SignerLimit = cs.Epoch, cs.SignerLimit
	go self.resetSignersLevel(hash, number)
	return nil
}

func (self *TribeStatus) resetSignersLevel(hash common.Hash, number *big.Int) {
	m := self.GetMinerAddress()
	for _, s := range self.Signers {
		if s.Address == m {
			self.SignerLevel = LevelSigner
			return
		}
	}
	for _, s := range self.blackList {
		if s == m {
			self.SignerLevel = LevelSinner
			return
		}
	}

	for _, s := range self.Leaders {
		if s == m {
			self.SignerLevel = LevelSigner
			return
		}
	}

	ci := params.GetChiefInfo(number)
	switch ci.Version {
	case "0.0.6":
		// if filterVolunteer return 1 then is volunteer
		rtn := params.SendToMsgBoxForFilterVolunteer(hash, number, m)
		r := <-rtn
		if r.Success {
			if fr := r.Entity.(*big.Int); fr != nil && fr.Int64() == 0 {
				self.SignerLevel = LevelVolunteer
				return
			}
		}
	}
	// default none
	self.SignerLevel = LevelNone
}

//?????????????????????
func (self *TribeStatus) loadSigners(sl []*Signer) {
	self.Signers = append(self.Signers[:0], sl...)
}

//InTurnForCalcDiffcultyChief100 ??????????????????inTurnForCalcChief100
func (self *TribeStatus) InTurnForCalcDiffcultyChief100(signer common.Address, parent *types.Header) *big.Int {
	return self.inTurnForCalcDifficultyChief100(parent.Number.Int64()+1, parent.Hash(), signer)
}

/*
inTurnForCalcDifficultyChief100 ?????????????????????????????????signer??????,???????????????????????????.
signers:[0,...,16] 0??????????????????????????????,1-16??????????????????????????????
??????1:
1. ?????????????????????????????????3,??????signer???3,??????????????????6.
2. ??????singers[0]??????????????????2, ???????????????2??????,?????????5,??????3???????????????4,...,??????1??????????????????1
??????2:???????????????????????????singers[0],??????????????????????????????
1. ??????signers[0] ??????,??????????????????6
2. ??????signers[0]?????????2,????????????3?????????????????????5,??????4????????????4,...??????1??????????????????2

?????????number?????????????????????????????????,???parentHash??????????????????????????????block?????????????????????
*/
func (self *TribeStatus) inTurnForCalcDifficultyChief100(number int64, parentHash common.Hash, signer common.Address) *big.Int {

	signers := self.Signers
	sl := len(signers)

	defer func() {
		log.Debug(fmt.Sprintf("inTurnForCalcDifficultyChief100  numer=%d,parentHash=%s signer=%s  ", number, parentHash.String(), signer.String()),
			"signers", signers)
	}()
	//	log.Info(fmt.Sprintf("singers=%v,signer=%s,leaders=%v,number=%d,parentHash=%s", signers, signer.String(), self.Leaders, number, parentHash.String()))
	if idx, _, err := self.fetchOnSigners(signer, signers); err == nil {
		// main
		if sl > 0 && number%int64(sl) == idx.Int64() {
			return big.NewInt(diff)
		}
		// second
		if idx.Int64() == 0 {
			return big.NewInt(diff - 1)
		}

	} else if sl > 0 {
		if leaders, err := leaderSort(signers[0].Address, self.Leaders); err == nil {
			for i, leader := range leaders {
				if signer == leader && number%int64(sl) == 0 {
					return big.NewInt(diff - int64(i+1))
				} else if signer == leader {
					return big.NewInt(diff - int64(i+2))
				}
			}
		}
	}
	return diffNoTurn
}

//InTurnForVerifyDifficultyChief100: ??????????????????inTurnForCalcChief100
func (self *TribeStatus) InTurnForVerifyDifficultyChief100(number int64, parentHash common.Hash, signer common.Address) *big.Int {
	return self.inTurnForCalcDifficultyChief100(number, parentHash, signer)
}

/*
??????list=[1,2,3,4,5]
first=3,????????????[4,5,1,2]
??????first=2,??????[3,4,5,1]
??????first=5,??????[1,2,3,4]
?????????????????????,?????????????????????????????????leader??????????????????,?????????????????????leader
*/
func leaderSort(first common.Address, list []common.Address) ([]common.Address, error) {
	for i, o := range list {
		if first == o {
			return append(list[i+1:], list[:i]...), nil
		}
	}

	return list, nil
}

//InTurnForCalcDifficulty ???0.6??????yiqian ????????????
func (self *TribeStatus) InTurnForCalcDifficulty(signer common.Address, parent *types.Header) *big.Int {
	number := parent.Number.Int64() + 1
	signers := self.Signers
	if idx, _, err := self.fetchOnSigners(signer, signers); err == nil {
		sl := len(signers)
		if params.IsSIP002Block(big.NewInt(number)) {
			if sl > 0 && number%int64(sl) == idx.Int64() {
				return diffInTurnMain
			} else if sl > 0 && (number+1)%int64(sl) == idx.Int64() {
				return diffInTurn
			}
		} else {
			if sl > 0 && number%int64(sl) == idx.Int64() {
				return diffInTurn
			}
		}
	}

	return diffNoTurn
}

//0.6????????????????????????
func (self *TribeStatus) InTurnForVerifyDiffculty(number int64, parentHash common.Hash, signer common.Address) *big.Int {
	if ci := params.GetChiefInfo(big.NewInt(number)); ci != nil {
		switch ci.Version {
		case "1.0.0":
			//TODO max value is a var ???
			return self.InTurnForVerifyDifficultyChief100(number, parentHash, signer)
		}
	}

	var signers []*Signer
	if number > 3 {
		signers = self.Signers
	} else {
		return diffInTurn
	}
	if idx, _, err := self.fetchOnSigners(signer, signers); err == nil {
		sl := len(signers)
		if params.IsSIP002Block(big.NewInt(number)) {
			if sl > 0 && number%int64(sl) == idx.Int64() {
				return diffInTurnMain
			} else if sl > 0 && (number+1)%int64(sl) == idx.Int64() {
				return diffInTurn
			}
		} else {
			if sl > 0 && number%int64(sl) == idx.Int64() {
				return diffInTurn
			}
		}
	}
	return diffNoTurn
}

func (self *TribeStatus) genesisSigner(header *types.Header) (common.Address, error) {
	extraVanity := extraVanityFn(header.Number)
	signer := common.Address{}
	copy(signer[:], header.Extra[extraVanity:])
	self.loadSigners([]*Signer{{signer, 3}})
	return signer, nil
}

//address?????????signer????????????signers????????????
func (self *TribeStatus) fetchOnSigners(address common.Address, signers []*Signer) (*big.Int, *Signer, error) {
	if signers == nil {
		signers = self.Signers
	}
	if l := len(signers); l > 0 {
		for i := 0; i < l; i++ {
			if s := signers[i]; s.Address == address {
				return big.NewInt(int64(i)), s, nil
			}
		}
	}
	return nil, nil, errors.New("not_found")
}

func verifyVrfNum(parent, header *types.Header) (err error) {
	var (
		np  = header.Extra[:extraVanityFn(header.Number)]
		sig = header.Extra[len(header.Extra)-extraSeal:]
		msg = append(parent.Number.Bytes(), parent.Extra[:32]...)
	)
	pubbuf, err := ecrecoverPubkey(header, sig)
	if err != nil {
		//panic(err) //???????????????panic,???????????????????????????????????????,???????????????????????????.
		return err
	}
	x, y := elliptic.Unmarshal(crypto.S256(), pubbuf)
	pubkey := ecdsa.PublicKey{Curve: crypto.S256(), X: x, Y: y}
	err = crypto.SimpleVRFVerify(&pubkey, msg, np)
	log.Debug("[verifyVrfNum]", "err", err, "num", header.Number, "vrfn", new(big.Int).SetBytes(np[:32]), "parent", header.ParentHash.Bytes())
	return
}

/*
validateSigner:
1. ??????????????????????????????,???????????????GetPeriodChief100??????
2.
*/
func (self *TribeStatus) validateSigner(parentHeader, header *types.Header, signer common.Address) bool {
	var (
		err     error
		signers = self.Signers
		number  = header.Number.Int64()
	)
	//if number > 1 && self.Number != parentNumber {
	if number <= CHIEF_NUMBER {
		return true
	}

	if params.IsSIP002Block(header.Number) {
		// second time of verification block time
		period := self.tribe.GetPeriod(header, signers)
		pt := parentHeader.Time.Uint64()
		if pt+period > header.Time.Uint64() {
			log.Error("[ValidateSigner] second time verification block time error", "num", header.Number, "pt", pt, "period", period, ", pt+period=", pt+period, " , ht=", header.Time.Uint64())
			log.Error("[ValidateSigner] second time verification block time error", "err", ErrInvalidTimestampSIP002)
			return false
		}
	}

	if params.IsSIP100Block(header.Number) && header.Coinbase == common.HexToAddress("0x") {
		log.Error("error_signer", "num", header.Number.String(), "miner", header.Coinbase.Hex(), "signer", signer.Hex())
		return false
	}

	idx, _, err := self.fetchOnSigners(signer, signers)
	if params.IsSIP100Block(header.Number) {
		if err == nil {
			// ???????????????????????????
			idx_m := number % int64(len(signers))
			if idx_m == idx.Int64() {
				return true
			}
			// ????????????????????????????????????
			if idx.Int64() == 0 {
				return true
			}
		} else {
			// other leader
			for _, leader := range self.Leaders {
				if signer == leader { //????????????????????????????????????????????????????????????????
					return true
				}
			}
		}
	} else if err == nil {
		return true
	}
	return false
}

/*
VerifySignerBalance: ???chief1.0???????????????????????????????????????????????????????????????,chief1.0???????????????????????????poc????????????????????????.
*/
//func (self *TribeStatus) VerifySignerBalance(state *state.StateDB, addr common.Address, header *types.Header) error {
//	// SIP100 skip this verify
//	if params.IsSIP100Block(header.Number) {
//		return nil
//	}
//	var (
//		pnum, num *big.Int
//	)
//	if addr == common.HexToAddress("0x") {
//		if _addr, err := ecrecover(header, self.tribe); err == nil {
//			addr = _addr
//		} else {
//			return err
//		}
//	}
//	if header != nil {
//		num = header.Number
//		pnum = new(big.Int).Sub(num, big.NewInt(1))
//	} else {
//		return errors.New("params of header can not be null")
//	}
//	// skip when v in meshbox.sol
//	if params.IsReadyMeshbox(pnum) && params.MeshboxExistAddress(addr) {
//		return nil
//	}
//
//	return nil
//
//}

// every block
// sync download or mine
// check chief tx
func (self *TribeStatus) ValidateBlock(parent, block *types.Block, validateSigner bool) error {
	if block.Number().Int64() <= CHIEF_NUMBER {
		return nil
	}
	var err error
	if validateSigner {
		//The miner updates the chife contract information when prepare, and the follower  updates the chief contract information whenValidateBlock.
		err = self.LoadStatusFromChief(parent.Hash(), block.Number())
		if err != nil {
			log.Error(fmt.Sprintf("[ValidateBlock] LoadSignersFromChief ,parent=%s,current=%s,currentNumber=%s", parent.Hash().String(), block.Hash().String(), block.Number()))
			return err
		}
	}

	header := block.Header()
	number := header.Number.Int64()

	//number := block.Number().Int64()
	// add by liangc : seal call this func must skip validate signer ???????????????????????????????????????
	if validateSigner {
		signer, err := ecrecover(header, self.tribe)
		// verify signer
		if err != nil {
			return err
		}
		// verify difficulty ?????????
		if !params.IsBeforeChief100block(header.Number) {
			difficulty := self.InTurnForVerifyDiffculty(number, header.ParentHash, signer)
			if difficulty.Cmp(header.Difficulty) != 0 {
				log.Error("** ValidateBlock ERROR **", "head.diff", header.Difficulty.String(), "target.diff", difficulty.String(), "err", errInvalidDifficulty, "validateFromSeal", !validateSigner)
				return errInvalidDifficulty
			}
		}
		// verify vrf num
		if params.IsSIP100Block(header.Number) {
			err = verifyVrfNum(parent.Header(), header)
			if err != nil {
				log.Error("vrf_num_fail", "num", number, "err", err)
				return err
			}
		}
		if !self.validateSigner(parent.Header(), header, signer) {
			return errUnauthorized
		}
	}
	// check first tx , must be chief.tx , and onely one chief.tx in tx list
	if block != nil && block.Transactions().Len() == 0 {
		return ErrTribeNotAllowEmptyTxList
	}

	var total = 0
	for i, tx := range block.Transactions() {
		from := types.GetFromByTx(tx)
		/*
			must verify tx.from ==signer:
			otherwise:
			if miner A minging the block#16,then A can make chief.update tx fail,
			so signerList will never update, A will make sure he can mine block for every round.
		*/
		if tx.To() != nil && params.IsChiefAddressOnBlock(block.Number(), *tx.To()) && params.IsChiefUpdate(tx.Data()) {
			if i != 0 {
				return ErrTribeChiefTxMustAtPositionZero
			}
			if validateSigner {
				signer, err := ecrecover(header, self.tribe)
				// verify signer
				if err != nil {
					return err
				}

				if from == nil || *from != signer {
					return ErrTribeChiefTxSignerAndBlockSignerNotMatch
				}

				if params.IsSIP100Block(header.Number) {
					// TODO SIP100 check volunteer by vrfnp
					volunteerHex := common.Bytes2Hex(tx.Data()[4:])
					volunteer := common.HexToAddress(volunteerHex)
					vrfn := header.Extra[:32]
					if !params.VerifyMiner(header.ParentHash, volunteer, vrfn) {
						return errors.New("verify_volunteer_fail")
					}
				}
			}
			total++
		}
	}
	if total == 0 {
		return ErrTribeMustContainChiefTx
	}

	log.Debug("ValidateBlockp-->", "num", block.NumberU64(), "check_signer", validateSigner)
	return nil
}

func (self *TribeStatus) String() string {
	if b, e := json.Marshal(self); e != nil {
		return "error: " + e.Error()
	} else {
		return "status: " + string(b)
	}
}
