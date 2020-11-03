---


---

<hr>
<h2 id="title-挑战赛积分不好拿？快来看看-tidb-bug-hunting">title: 挑战赛积分不好拿？快来看看 TiDB bug hunting</h2>
<p>继 <a href="https://mp.weixin.qq.com/s/_l_wLW2IVnrYTHVvZjR1FA">TiDB 4.0 捉"虫"竞赛</a>后，大家在 4.0 的 TiDB 中陆续发现了一些 bug，有且不限于：</p>
<ul>
<li>和 MySQL 的兼容性问题</li>
<li>新功能之间的兼容性问题</li>
<li>正确性问题</li>
<li>…</li>
</ul>
<p>为了共建 TiDB 良好的稳定性，我们决定进行新一轮的 TiDB Challenge Program。这次的主题是 TiDB bug hunting，同学们可以参与修复目前 TiDB 已发现的种种 bug，既维护了 TiDB 的稳定性，又积累了硬核大型数据库的开发经验，同时还有高额<a href="https://mp.weixin.qq.com/s/_l_wLW2IVnrYTHVvZjR1FA">挑战赛积分</a>可以拿哦。</p>
<p>欢迎大家加入 TiDB Community Slack Workspace（点击【阅读原文】加入），过程中遇到任何问题都可以直接通过 #sig-planner 或者 #sig-exec 的 channel 与我们取得联系。</p>
<h2 id="如何参与">如何参与</h2>
<h3 id="准备">准备</h3>
<ul>
<li>
<p>参考 <a href="https://github.com/join">Join GitHub</a> 完成 GitHub 账号的创建。</p>
</li>
<li>
<p>参考 <a href="https://git-scm.com/book/en/v2/Getting-Started-Installing-Git/">Installing Git</a> 在本地环境中安装 Git。</p>
</li>
<li>
<p>通过 <a href="https://git-scm.com/book/en/v2/Getting-Started-First-Time-Git-Setup">Set up Git</a> 配置 Git 访问 GitHub。</p>
</li>
</ul>
<h3 id="参与">参与</h3>
<p>参赛全流程包括：查看任务-&gt;领取任务-&gt;实现任务-&gt;提交任务-&gt;评估任务-&gt;获得积分-&gt;积分兑换，其中“获得积分”之前的步骤都将在 GitHub 上实现。</p>
<p><strong>第一步：查看 Issue</strong></p>
<p>开放的 Issue 列表可以直接在 <a href="https://github.com/pingcap/tidb/issues">tidb/issues</a> 中看到，这个页面打开后会出现一个 pinned issue “Welcome contributors”。</p>
<p>pinned 图</p>
<p>Pinned issue 里面记录的带有 <em>challenge-program</em> 的标签的 issue 都是可以参与完成的。</p>
<p>pinned detail 图</p>
<blockquote>
<p><strong>注</strong>：<br>
目前我们只开放了 planner 和 execution 模块的 issue，其他的模块将在接下来陆续开放。</p>
</blockquote>
<p><strong>第二步：领取任务</strong></p>
<p>如果你决定认领某一个 issue，可以先在这个 Issue 中回复 “/pick-up”，bot 会帮你打上 “picked” 的标签，并告诉你的是否领取成功。</p>
<p><img src="https://i.loli.net/2020/11/03/gDM8y19AQOUTX2m.jpg" alt="pick.jpg"></p>
<p><strong>第三步：实现代码</strong></p>
<p>在实现代码的过程中如果遇到问题，可以通过 #sig-planner 或 #sig-exec channel 与我们进行探讨，Issue 指定的 Mentor 会尽可能在 24h 内予以回复。不过，在提出问题之前一定要确保你已经仔细阅读过题目内容，并且已经完成了参考资料的学习哦～</p>
<p><strong>第四步：提交代码</strong></p>
<p>如果你觉得你的方案已经达到了题目的要求，可在相关 Repo（例如 tidb）的 master 分支上实现你的方案，并将代码以 GitHub Pull Request（简称 PR）的形式提交到相应的 GitHub Repo 上。当 PR 提交后，可在 PR 的评论中 使用 “/cc @Mentor” at Mentor 进行代码评审，Mentor 会尽可能在方案提交后的 48h 内完成评估。</p>
<p><img src="https://i.loli.net/2020/11/03/lMxoAuDvWzpm352.jpg" alt="cc.jpg"></p>
<p><strong>第五步：代码评估及积分授予</strong></p>
<p><strong>评估规则</strong>：PR Reviewer 会对 PR 进行代码格式、代码功能和性能的 Review，获得 2 个以上 Reviewer 认可（即在 PR 中评论“LGTM”）的 PR 将会被 merge 到对应 repo 的主干。</p>
<p><img src="https://i.loli.net/2020/11/03/jWk7O492CRfHJlD.jpg" alt="LGTM.jpg"></p>
<p>如果你的 PR 被 Merge 到 Master 分支，那么就意味着该题目被你挑战成功，你将获得该题目对应的积分；其他参赛选手将失去对该题目的挑战资格，已经提交的 PR 也会被 Close。</p>
<p>否则，你需要继续和 PR 的 Reviewer 探讨实现方案和细节，合理的接受或者拒绝 Reviewer 对 PR 的评审建议。</p>
<p>另外，有些 bug-fix 的 PR 会被认定为需要 cherry-pick 到指定版本，Bot 和 Reviewer 会帮助进行这个步骤，在你的 PR merge 到 Master 分支后，Bot 会创建 cherry-pick 的 PR，我们也建议你关注并帮助解决这些 PR 的冲突。</p>
<p><img src="https://i.loli.net/2020/11/03/urO1axqjtUWyJT3.jpg" alt="cherry-pick.jpg"></p>
<p><strong>第六步：积分兑换</strong></p>
<p>积分获得情况将会在  TiDB 性能挑战赛官方网站  呈现。所获积分可兑换礼品或奖金，礼品包括但不限于：TiDB 限量版帽衫、The North Face 定制电脑双肩包等。</p>
<ul>
<li>
<p>兑换时间：每个赛季结束后至下一赛季结束前可进行积分兑换，下一个赛季结束时，前一赛季的可兑换积分将直接清零，不可再进行社区礼品兑换。</p>
</li>
<li>
<p>兑换方式：本赛季结束后填写礼品兑换表（届时将开放填写权限）。</p>
</li>
</ul>
<h2 id="学习资料">学习资料</h2>
<p>这里有 <a href="https://github.com/pingcap/presentations/blob/master/hackathon-2019/reference-document-of-hackathon-2019.md">TiDB 精选技术讲解文章</a>，帮助大家轻松掌握 TiDB 各核心组件的原理及功能。推荐学习 <a href="https://space.bilibili.com/86485707/channel/detail?cid=145009">High Perfromance TiDB 课程</a>，对 bug hunting 更有帮助。还有 <a href="https://github.com/pingcap/awesome-database-learning">数据库小课堂</a>，帮助大家快速熟悉数据库知识，点击以上链接即可轻松获取。</p>

