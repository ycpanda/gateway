/* ©INFINI, All Rights Reserved.
 * mail: contact#infini.ltd */

package transform

import (
	"fmt"
	"infini.sh/framework/core/config"
	"infini.sh/framework/core/pipeline"
	"infini.sh/framework/core/util"
	"infini.sh/framework/lib/fasthttp"
	log "github.com/cihub/seelog"
)

type SetContext struct {
	Context map[string]interface{} `config:"context"`
}

func (filter *SetContext) Name() string {
	return "set_context"
}

func (filter *SetContext) Filter(ctx *fasthttp.RequestCtx) {
	if len(filter.Context)>0{
		keys:=util.Flatten(filter.Context,false)
		for k,v:=range keys{
			fmt.Println("upate:",k,":",v)
			err:=ctx.SetValue(k,util.ToString(v))
			if err!=nil{
				log.Error(err)
			}
		}
	}
}

func NewSetContext(c *config.Config) (pipeline.Filter, error) {

	runner := SetContext{
	}

	if err := c.Unpack(&runner); err != nil {
		return nil, fmt.Errorf("failed to unpack the filter configuration : %s", err)
	}

	return &runner, nil
}
