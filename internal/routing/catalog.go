package routing

import "fmt"

// CatalogChannel 路由目标候选：一个渠道及其凭证分组，由各 provider 从自己的存储构造。
type CatalogChannel struct {
	ID       int64
	Name     string
	Priority int
	// Paused 渠道已暂停（不参与渠道间故障转移），命中时该目标会被跳过
	Paused bool
	// Active 当前全局激活渠道
	Active bool
	// NonStream 渠道自身的非流改道配置；非 nil 说明它声明了不接非流请求
	NonStream *NonStreamRoute
	// PrivacyLevel 渠道隐私级别（third_party / trusted），空串为跟随全局
	PrivacyLevel string
	Groups       []CatalogGroup
}

// CatalogGroup 渠道下的凭证分组
type CatalogGroup struct {
	ID   int64
	Name string
}

// Catalog 一个 provider 的全部路由目标候选，按渠道优先级排列
type Catalog []CatalogChannel

// Channel 按 ID 查找渠道，不存在时返回 nil
func (c Catalog) Channel(id int64) *CatalogChannel {
	for i := range c {
		if c[i].ID == id {
			return &c[i]
		}
	}
	return nil
}

// Active 返回当前激活渠道，不存在时返回 nil
func (c Catalog) Active() *CatalogChannel {
	for i := range c {
		if c[i].Active {
			return &c[i]
		}
	}
	return nil
}

// GroupName 返回凭证分组名；groupID 为 0 或分组不存在时返回空串
func (ch *CatalogChannel) GroupName(groupID int64) string {
	if ch == nil || groupID <= 0 {
		return ""
	}
	for _, group := range ch.Groups {
		if group.ID == groupID {
			return group.Name
		}
	}
	return ""
}

// Validate 校验目标都指向本 provider 下存在的渠道，且指定的凭证分组属于该渠道
func (c Catalog) Validate(targets []Target) error {
	for i, target := range targets {
		channel := c.Channel(target.ChannelID)
		if channel == nil {
			return fmt.Errorf("第 %d 个目标的渠道不存在（ID=%d）", i+1, target.ChannelID)
		}
		if target.GroupID > 0 && channel.GroupName(target.GroupID) == "" {
			return fmt.Errorf("第 %d 个目标的凭证分组不属于渠道 %s（分组 ID=%d）", i+1, channel.Name, target.GroupID)
		}
	}
	return nil
}
