
function hide_class(cls){var e=document.getElementsByClassName(cls);for(var i=0;i<e.length;i++){if(e[i].classList.contains('Button'))continue;e[i].style.display=(e[i].style.display==='none')?'':'none';}}
function hide_priority(p){hide_class(p);}
function hide(id){var e=document.getElementById(id);if(e)e.style.display=(e.style.display==='none')?'':'none';}
